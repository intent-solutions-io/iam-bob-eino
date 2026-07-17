package cli

// The offline deterministic lifecycle e2e: a real git fixture repository, the
// scripted offline model stub, and the full plan → run → verify → evidence
// surface driven through cli.Run exactly as an operator would. No network, no
// credentials, no wall-clock dependence (injected clock).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/intent-solutions-io/iam-bob-eino/internal/config"
	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/gitstate"
	"github.com/intent-solutions-io/iam-bob-eino/internal/plan"
	"github.com/intent-solutions-io/iam-bob-eino/internal/provider"
	"github.com/intent-solutions-io/iam-bob-eino/internal/receipt"
)

const greetingContent = "hello world\n"

// gitFixture builds a committed git repository containing greeting.txt and
// pins the lifecycle env; returns the repo path and its HEAD SHA.
func gitFixture(t *testing.T) (repo, headSHA string) {
	t.Helper()
	lifecycleEnv(t) // pins XDG state/config + clears provider env
	repo = t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid",
			"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "greeting.txt"), []byte(greetingContent), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "fixture")
	st, err := gitstate.Head(repo)
	if err != nil {
		t.Fatal(err)
	}
	// The workspace layer resolves symlinks (macOS/tmp), so match it.
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		repo = resolved
	}
	return repo, st.HeadSHA
}

// scriptFake installs a fully-configurable offline model stub.
func scriptFake(t *testing.T, fixture *provider.FakeChatModel) {
	t.Helper()
	orig := modelFactory
	modelFactory = func(context.Context, config.Config) (einomodel.ToolCallingChatModel, error) {
		return fixture, nil
	}
	t.Cleanup(func() { modelFactory = orig })
}

func toolCall(id, name, args string) *schema.Message {
	return schema.AssistantMessage("", []schema.ToolCall{{
		ID: id, Type: "function",
		Function: schema.FunctionCall{Name: name, Arguments: args},
	}})
}

func hexSHA(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// e2eDraft is the plan draft Script A's model produces.
func e2eDraft() string {
	return `{"proposed_actions":["update the greeting"],
"proposed_files":["greeting.txt"],
"proposed_commands":["git status"],
"required_capabilities":["writes","exec"],
"acceptance_checks":["git status"],
"risks":["low"],
"assumptions":["fixture repo"],
"questions":[]}`
}

// planViaCLI runs Script A (list → read → search → JSON draft) and returns
// the plan id.
func planViaCLI(t *testing.T, repo string) string {
	t.Helper()
	fixedClock(t)
	scriptFake(t, provider.NewFake(
		toolCall("c1", "list_dir", `{"path":""}`),
		toolCall("c2", "read_file", `{"path":"greeting.txt"}`),
		toolCall("c3", "search_code", `{"pattern":"hello"}`),
		schema.AssistantMessage(e2eDraft(), nil),
	))
	var stdout, stderr bytes.Buffer
	code := Run([]string{"plan", "-workspace", repo, "improve the greeting"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("plan exit = %d\nstderr:\n%s", code, stderr.String())
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if strings.HasPrefix(line, "plan_id: ") {
			return strings.TrimPrefix(line, "plan_id: ")
		}
	}
	t.Fatal("no plan_id in plan output")
	return ""
}

// patchArgs builds the apply_patch tool arguments editing greeting.txt.
func patchArgs(t *testing.T) string {
	t.Helper()
	doc := map[string]any{
		"schema_version": "intent-bob-eino-patch/v1",
		"files": []map[string]any{{
			"path": "greeting.txt", "pre_sha256": hexSHA(greetingContent),
			"hunks": []map[string]any{{"find": "hello world", "replace": "hello, governed world", "expect_count": 1, "occurrence": 0}},
		}},
	}
	docJSON, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]string{"patch_json": string(docJSON)})
	if err != nil {
		t.Fatal(err)
	}
	return string(args)
}

// TestE2EPlanScriptProducesSHAPinnedArtifact is Script A.
func TestE2EPlanScriptProducesSHAPinnedArtifact(t *testing.T) {
	repo, head := gitFixture(t)
	planID := planViaCLI(t, repo)

	path := filepath.Join(PlansDir(), planID+".json")
	if strings.HasPrefix(path, repo) {
		t.Fatal("plan artifact stored inside the workspace")
	}
	p, err := plan.Load(path)
	if err != nil {
		t.Fatalf("plan round-trip: %v", err)
	}
	if p.WorkspaceStartSHA != head {
		t.Errorf("plan start SHA = %s, want fixture HEAD %s", p.WorkspaceStartSHA, head)
	}
	if p.WorkspaceIdentity != repo {
		t.Errorf("workspace identity = %s, want %s", p.WorkspaceIdentity, repo)
	}
}

// TestE2ERunAppliesPatchRunsAcceptanceAndVerifies is Script B plus the
// verify and evidence commands over its output.
func TestE2ERunAppliesPatchRunsAcceptanceAndVerifies(t *testing.T) {
	repo, _ := gitFixture(t)
	planID := planViaCLI(t, repo)

	scriptFake(t, provider.NewFake(
		toolCall("c1", "apply_patch", patchArgs(t)),
		toolCall("c2", "run_command", `{"command":"git status"}`),
		schema.AssistantMessage("patched greeting.txt and ran the acceptance check", nil),
	))
	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "-plan", planID, "-workspace", repo, "-allow-writes", "-allow-exec", "-yes"},
		strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit = %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}

	// The in-plan patch landed.
	got, err := os.ReadFile(filepath.Join(repo, "greeting.txt"))
	if err != nil || !strings.Contains(string(got), "governed world") {
		t.Fatalf("patched content = %q (%v)", got, err)
	}

	// Exactly one sealed receipt; loads tamper-free; bound to the plan.
	entries, err := os.ReadDir(ReceiptsDir())
	if err != nil || len(entries) != 1 {
		t.Fatalf("receipts: %v / %d", err, len(entries))
	}
	r, err := receipt.Load(filepath.Join(ReceiptsDir(), entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if r.PlanID != planID || r.PatchesApplied != 1 || r.FinalStatus != "verified" {
		t.Errorf("receipt: plan=%s patches=%d status=%s", r.PlanID, r.PatchesApplied, r.FinalStatus)
	}
	if len(r.FilesChanged) != 1 || r.FilesChanged[0] != "greeting.txt" {
		t.Errorf("files changed = %v", r.FilesChanged)
	}

	// verify + evidence pass over the same state.
	var vso, vse bytes.Buffer
	if code := Run([]string{"verify", "-receipt", r.RunID, "-plan", planID}, strings.NewReader(""), &vso, &vse); code != 0 {
		t.Fatalf("verify exit = %d\n%s\n%s", code, vso.String(), vse.String())
	}
	var eso, ese bytes.Buffer
	if code := Run([]string{"evidence", "verify-chain"}, strings.NewReader(""), &eso, &ese); code != 0 {
		t.Fatalf("evidence verify-chain exit != 0:\n%s", ese.String())
	}
}

// TestE2EVarianceWriteUnderYesIsDenied: -yes must not authorize an
// out-of-plan write; the file must not exist and the denial must be recorded.
func TestE2EVarianceWriteUnderYesIsDenied(t *testing.T) {
	repo, _ := gitFixture(t)
	planID := planViaCLI(t, repo)

	scriptFake(t, provider.NewFake(
		toolCall("c1", "write_file", `{"path":"evil.txt","content":"outside the plan"}`),
		toolCall("c2", "run_command", `{"command":"git status"}`),
		schema.AssistantMessage("attempted an extra file; ran acceptance", nil),
	))
	var stdout, stderr bytes.Buffer
	Run([]string{"run", "-plan", planID, "-workspace", repo, "-allow-writes", "-allow-exec", "-yes"},
		strings.NewReader(""), &stdout, &stderr)

	if _, err := os.Stat(filepath.Join(repo, "evil.txt")); !os.IsNotExist(err) {
		t.Fatal("variance write landed under -yes — the guard is broken")
	}
	recs := readRunEvidence(t)
	var denied bool
	for _, rec := range recs {
		if rec.Tool.Name == "write_file" && rec.Authorization == "denied" &&
			strings.Contains(rec.ExecutionInfo, "variance") {
			denied = true
		}
	}
	if !denied {
		t.Errorf("no variance denial in evidence: %+v", recs)
	}
}

// TestE2ERepeatedIdenticalCallsTripTheLimit: the stuck-loop signature ends
// the run with the typed limit status and a sealed receipt.
func TestE2ERepeatedIdenticalCallsTripTheLimit(t *testing.T) {
	repo, _ := gitFixture(t)
	planID := planViaCLI(t, repo)

	same := func(id string) *schema.Message { return toolCall(id, "read_file", `{"path":"greeting.txt"}`) }
	scriptFake(t, provider.NewFake(same("c1"), same("c2"), same("c3"), same("c4"), same("c5")))
	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "-plan", planID, "-workspace", repo, "-allow-writes", "-allow-exec", "-yes"},
		strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("a tripped limit must exit non-zero")
	}
	if !strings.Contains(stdout.String(), "final_status: limit_exhausted:max_repeated_identical") {
		t.Fatalf("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	// Evidence preserved: the three allowed reads AND the denied fourth.
	recs := readRunEvidence(t)
	if len(recs) < 4 {
		t.Fatalf("evidence records = %d, want the allowed calls plus the denial", len(recs))
	}
	last := recs[len(recs)-1]
	if last.Authorization != "denied" || !strings.Contains(last.ExecutionInfo, "max_repeated_identical") {
		t.Errorf("final record: %+v", last)
	}
}

// TestE2EProviderErrorMidRunSealsTypedReceipt: an injected turn-1 provider
// failure ends the run as provider_error with the turn-0 evidence intact.
func TestE2EProviderErrorMidRunSealsTypedReceipt(t *testing.T) {
	repo, _ := gitFixture(t)
	planID := planViaCLI(t, repo)

	fixture := provider.NewFake(toolCall("c1", "read_file", `{"path":"greeting.txt"}`))
	fixture.Errors = map[int]error{1: errors.New("provider request failed: 429 too many requests")}
	scriptFake(t, fixture)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "-plan", planID, "-workspace", repo, "-allow-writes", "-allow-exec", "-yes"},
		strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("provider failure must exit non-zero")
	}
	if !strings.Contains(stdout.String(), "final_status: provider_error") {
		t.Fatalf("stdout:\n%s", stdout.String())
	}
	if recs := readRunEvidence(t); len(recs) != 1 || recs[0].Tool.Name != "read_file" {
		t.Errorf("pre-failure evidence must be preserved: %+v", recs)
	}
}

// TestE2EBlockedTurnHitsTheTimeout: a context-blocking model turn plus a
// short -timeout classifies as timeout, never as success.
func TestE2EBlockedTurnHitsTheTimeout(t *testing.T) {
	repo, _ := gitFixture(t)
	planID := planViaCLI(t, repo)

	fixture := provider.NewFake()
	fixture.BlockTurn = 1
	scriptFake(t, fixture)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"run", "-plan", planID, "-workspace", repo, "-allow-writes", "-allow-exec", "-yes", "-timeout", "300ms"},
		strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("a timed-out run must exit non-zero")
	}
	if !strings.Contains(stdout.String(), "final_status: timeout") {
		t.Fatalf("stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
}

// TestE2EMalformedToolArgsAreRecordedAsErrors: a malformed apply_patch
// document is an evidence-recorded tool error, not a crash and not a write.
func TestE2EMalformedToolArgsAreRecordedAsErrors(t *testing.T) {
	repo, _ := gitFixture(t)
	planID := planViaCLI(t, repo)

	badArgs, _ := json.Marshal(map[string]string{"patch_json": `{"schema_version":"wrong/v9"}`})
	scriptFake(t, provider.NewFake(
		toolCall("c1", "apply_patch", string(badArgs)),
		toolCall("c2", "run_command", `{"command":"git status"}`),
		schema.AssistantMessage("patch was malformed; acceptance still ran", nil),
	))
	var stdout, stderr bytes.Buffer
	Run([]string{"run", "-plan", planID, "-workspace", repo, "-allow-writes", "-allow-exec", "-yes"},
		strings.NewReader(""), &stdout, &stderr)

	recs := readRunEvidence(t)
	var sawError bool
	for _, rec := range recs {
		if rec.Tool.Name == "apply_patch" && rec.Execution == "error" {
			sawError = true
		}
	}
	if !sawError {
		t.Errorf("malformed patch must be an evidence-recorded error: %+v", recs)
	}
	if got, _ := os.ReadFile(filepath.Join(repo, "greeting.txt")); string(got) != greetingContent {
		t.Error("malformed patch mutated the workspace")
	}
}

// readRunEvidence loads the canonical evidence log of the current test env,
// keeping only run-correlated records (the run command binds Corr to the
// "run-…" run id; planning evidence keeps its own correlation).
func readRunEvidence(t *testing.T) []evidence.Record {
	t.Helper()
	records, err := receipt.LoadEvidenceLog(filepath.Join(StateDir(), "evidence.jsonl"))
	if err != nil {
		t.Fatalf("evidence load: %v", err)
	}
	var out []evidence.Record
	for _, r := range records {
		if strings.HasPrefix(r.CorrelationID, "run-") {
			out = append(out, r)
		}
	}
	return out
}
