package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/intent-solutions-io/iam-bob-eino/internal/config"
	"github.com/intent-solutions-io/iam-bob-eino/internal/plan"
	"github.com/intent-solutions-io/iam-bob-eino/internal/receipt"
)

// erroringModel fails every generation with a rate-limit-shaped error — the
// provider_error classification fixture.
type erroringModel struct{}

func (erroringModel) Generate(context.Context, []*schema.Message, ...einomodel.Option) (*schema.Message, error) {
	return nil, errors.New("provider request failed: 429 too many requests")
}

func (erroringModel) Stream(context.Context, []*schema.Message, ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("provider request failed: 429 too many requests")
}

func (m erroringModel) WithTools([]*schema.ToolInfo) (einomodel.ToolCallingChatModel, error) {
	return m, nil
}

func failFactory(t *testing.T) {
	t.Helper()
	orig := modelFactory
	modelFactory = func(context.Context, config.Config) (einomodel.ToolCallingChatModel, error) {
		return erroringModel{}, nil
	}
	t.Cleanup(func() { modelFactory = orig })
}

// savePlan finalizes and saves a plan into the (env-pinned) PlansDir.
func savePlan(t *testing.T, p plan.Plan) string {
	t.Helper()
	p.SchemaVersion = plan.SchemaVersion
	p.Status = "proposed"
	p.Authority = plan.AuthorityLocalUntrusted
	p.Finalize()
	if _, err := plan.Save(p, PlansDir()); err != nil {
		t.Fatalf("save plan fixture: %v", err)
	}
	return p.PlanID
}

// minimalPlan returns a runnable plan for ws: no file changes, one acceptance
// check that exits 0 anywhere.
func minimalPlan(ws string) plan.Plan {
	return plan.Plan{
		Task:                 "prove the lifecycle",
		WorkspaceIdentity:    ws,
		Provider:             "minimax",
		Model:                "MiniMax-M3",
		CreatedAt:            "2026-07-16T12:00:00Z",
		ProposedActions:      []string{"run the acceptance check"},
		RequiredCapabilities: []string{"exec", "writes"},
		AcceptanceChecks:     []string{"go version"},
	}
}

func TestRunHappyPathSealsVerifiedReceipt(t *testing.T) {
	ws := lifecycleEnv(t)
	fixedClock(t)
	planID := savePlan(t, minimalPlan(ws))
	scriptModel(t,
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "call_1", Type: "function",
			Function: schema.FunctionCall{Name: "run_command", Arguments: `{"command":"go version"}`},
		}}),
		schema.AssistantMessage("ran the acceptance check; exit 0", nil),
	)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "-plan", planID, "-workspace", ws, "-allow-writes", "-allow-exec", "-yes"},
		strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "final_status: verified") {
		t.Fatalf("stdout:\n%s", out)
	}

	entries, err := os.ReadDir(ReceiptsDir())
	if err != nil || len(entries) != 1 {
		t.Fatalf("receipts dir: %v / %d entries", err, len(entries))
	}
	r, err := receipt.Load(filepath.Join(ReceiptsDir(), entries[0].Name()))
	if err != nil {
		t.Fatalf("receipt.Load (tamper check included): %v", err)
	}
	if r.PlanID != planID || r.PlanHash == "" {
		t.Errorf("receipt plan binding: %+v", r)
	}
	if r.AgentIdentity == nil || r.AgentIdentity.RunID != r.RunID {
		t.Errorf("receipt identity/run binding: id=%+v run=%s", r.AgentIdentity, r.RunID)
	}
	if r.VerifierResult != "verified" || r.FinalStatus != "verified" {
		t.Errorf("verdict fields: %+v", r)
	}
	if len(r.TestResults) != 1 || r.TestResults[0] != "go version: exit=0" {
		t.Errorf("test results = %v", r.TestResults)
	}
	if r.ToolCalls < 1 || len(r.CommandsRun) != 1 {
		t.Errorf("evidence summary: calls=%d commands=%v", r.ToolCalls, r.CommandsRun)
	}
}

func TestRunPreflightWorkspaceMismatch(t *testing.T) {
	ws := lifecycleEnv(t)
	planID := savePlan(t, minimalPlan(ws))
	other := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "-plan", planID, "-workspace", other, "-allow-writes", "-allow-exec", "-yes"},
		strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("workspace mismatch must fail pre-flight")
	}
	if !strings.Contains(stderr.String(), "workspace identity mismatch") {
		t.Errorf("stderr:\n%s", stderr.String())
	}
}

func TestRunPreflightMissingCapabilityNamesTheFlag(t *testing.T) {
	ws := lifecycleEnv(t)
	planID := savePlan(t, minimalPlan(ws))
	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "-plan", planID, "-workspace", ws, "-yes"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("missing capability must fail pre-flight")
	}
	if !strings.Contains(stderr.String(), "-allow-writes") && !strings.Contains(stderr.String(), "-allow-exec") {
		t.Errorf("error must name the missing flag:\n%s", stderr.String())
	}
}

func TestRunPreflightProviderModelMustMatchPlan(t *testing.T) {
	ws := lifecycleEnv(t)
	p := minimalPlan(ws)
	p.Provider, p.Model = "deepseek", "deepseek-chat"
	planID := savePlan(t, p)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "-plan", planID, "-workspace", ws, "-allow-writes", "-allow-exec", "-yes"},
		strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("provider/model mismatch must fail pre-flight")
	}
	if !strings.Contains(stderr.String(), "deepseek/deepseek-chat") {
		t.Errorf("error should show the plan's selector:\n%s", stderr.String())
	}
}

func TestRunPreflightStaleSHAIsPlanInvalidated(t *testing.T) {
	ws := lifecycleEnv(t)
	p := minimalPlan(ws)
	p.WorkspaceStartSHA = strings.Repeat("d", 40) // no repo → HEAD unreadable
	planID := savePlan(t, p)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "-plan", planID, "-workspace", ws, "-allow-writes", "-allow-exec", "-yes"},
		strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("SHA-pinned plan on a mismatched workspace must fail")
	}
	if !strings.Contains(stderr.String(), "plan_invalidated") {
		t.Errorf("stderr:\n%s", stderr.String())
	}
}

func TestRunTamperedPlanFileRefused(t *testing.T) {
	ws := lifecycleEnv(t)
	planID := savePlan(t, minimalPlan(ws))
	path := filepath.Join(PlansDir(), planID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(raw, []byte("prove the lifecycle"), []byte("prove the lifecycle!"), 1)
	if err := os.WriteFile(path, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "-plan", planID, "-workspace", ws, "-allow-writes", "-allow-exec", "-yes"},
		strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("a tampered plan file must refuse to load")
	}
	if !strings.Contains(stderr.String(), "content_hash") {
		t.Errorf("stderr:\n%s", stderr.String())
	}
}

func TestRunProviderErrorIsTypedNonSuccess(t *testing.T) {
	ws := lifecycleEnv(t)
	fixedClock(t)
	planID := savePlan(t, minimalPlan(ws))
	// Rate-limit-shaped provider failure on the FIRST model turn: the run
	// must stop cleanly with the typed status — one attempt, no retry.
	failFactory(t)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "-plan", planID, "-workspace", ws, "-allow-writes", "-allow-exec", "-yes"},
		strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("provider error must exit non-zero")
	}
	if !strings.Contains(stdout.String(), "final_status: provider_error") {
		t.Errorf("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	entries, err := os.ReadDir(ReceiptsDir())
	if err != nil || len(entries) != 1 {
		t.Fatalf("a failed run must still seal a receipt: %v / %d", err, len(entries))
	}
	r, err := receipt.Load(filepath.Join(ReceiptsDir(), entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if r.ExecutionResult != "provider_error" || r.FinalStatus != "provider_error" {
		t.Errorf("receipt statuses: %+v", r)
	}
}
