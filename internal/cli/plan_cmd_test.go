package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/intent-solutions-io/iam-bob-eino/internal/config"
	"github.com/intent-solutions-io/iam-bob-eino/internal/plan"
	"github.com/intent-solutions-io/iam-bob-eino/internal/provider"
)

// lifecycleEnv pins state/config env for a hermetic subcommand test and
// returns a workspace directory.
func lifecycleEnv(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, v := range []string{"INTENT_BOB_EINO_PROVIDER", "INTENT_BOB_EINO_MODEL", "INTENT_BOB_EINO_CONFIG",
		"INTENT_BOB_EINO_BASE_URL", "INTENT_BOB_EINO_WORKSPACE", "INTENT_BOB_EINO_EVIDENCE_DIR",
		"BOB_PROVIDER", "BOB_MODEL", "BOB_CONFIG", "MINIMAX_BASE_URL", "MINIMAX_MODEL", "MINIMAX_API_KEY"} {
		t.Setenv(v, "")
	}
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "hello.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws
}

// scriptModel swaps the model factory for a deterministic Eino model fixture
// for the duration of one test.
func scriptModel(t *testing.T, msgs ...*schema.Message) {
	t.Helper()
	orig := modelFactory
	modelFactory = func(context.Context, config.Config) (einomodel.ToolCallingChatModel, error) {
		return provider.NewFake(msgs...), nil
	}
	t.Cleanup(func() { modelFactory = orig })
}

func fixedClock(t *testing.T) {
	t.Helper()
	orig := now
	now = func() time.Time { return time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { now = orig })
}

const goodDraft = `{"proposed_actions":["update the greeting"],
"proposed_files":["hello.txt"],
"proposed_commands":["git status"],
"required_capabilities":["writes"],
"acceptance_checks":["git status"],
"risks":["none"],
"assumptions":["workspace is the fixture"],
"questions":[]}`

func TestPlanSavesHashedArtifactOutsideWorkspace(t *testing.T) {
	ws := lifecycleEnv(t)
	fixedClock(t)
	scriptModel(t,
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "call_1", Type: "function",
			Function: schema.FunctionCall{Name: "read_file", Arguments: `{"path":"hello.txt"}`},
		}}),
		schema.AssistantMessage(goodDraft, nil),
	)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"plan", "-workspace", ws, "improve the greeting"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("plan exit = %d\nstderr:\n%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "plan_id: plan-") {
		t.Fatalf("stdout missing plan_id:\n%s", out)
	}
	planID := strings.TrimSpace(strings.TrimPrefix(strings.SplitN(out, "\n", 2)[0], "plan_id: "))
	path := filepath.Join(PlansDir(), planID+".json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("plan artifact not at %s: %v", path, err)
	}
	if strings.HasPrefix(path, ws) {
		t.Fatalf("plan artifact %s is inside the workspace %s", path, ws)
	}

	p, err := plan.Load(path)
	if err != nil {
		t.Fatalf("plan.Load round-trip: %v", err)
	}
	if p.Task != "improve the greeting" {
		t.Errorf("task = %q", p.Task)
	}
	if p.WorkspaceIdentity == "" || p.Authority != plan.AuthorityLocalUntrusted {
		t.Errorf("identity/authority: %+v", p)
	}
	// Declared "writes" ∪ inferred: acceptance checks imply exec.
	if got := strings.Join(p.RequiredCapabilities, ","); got != "exec,writes" {
		t.Errorf("required capabilities = %q, want \"exec,writes\"", got)
	}
	if p.CreatedAt != "2026-07-16T12:00:00Z" {
		t.Errorf("CreatedAt = %q, want the injected clock", p.CreatedAt)
	}
}

func TestPlanIdenticalContentYieldsIdenticalID(t *testing.T) {
	ws := lifecycleEnv(t)
	fixedClock(t)
	ids := make([]string, 2)
	for i := range ids {
		scriptModel(t, schema.AssistantMessage(goodDraft, nil))
		var stdout, stderr bytes.Buffer
		if code := Run([]string{"plan", "-workspace", ws, "improve the greeting"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Fatalf("plan run %d exit = %d\n%s", i, code, stderr.String())
		}
		ids[i] = strings.TrimSpace(strings.TrimPrefix(strings.SplitN(stdout.String(), "\n", 2)[0], "plan_id: "))
	}
	if ids[0] != ids[1] {
		t.Errorf("identical plan content produced different ids: %s vs %s", ids[0], ids[1])
	}
}

func TestPlanMalformedDraftWritesNothing(t *testing.T) {
	ws := lifecycleEnv(t)
	scriptModel(t, schema.AssistantMessage("I would edit some files and run some tests.", nil))
	var stdout, stderr bytes.Buffer
	code := Run([]string{"plan", "-workspace", ws, "do a thing"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("malformed draft must fail the plan command")
	}
	if !strings.Contains(stderr.String(), "not a valid plan JSON object") {
		t.Errorf("stderr should carry the typed draft error:\n%s", stderr.String())
	}
	if entries, err := os.ReadDir(PlansDir()); err == nil && len(entries) > 0 {
		t.Errorf("malformed draft must write nothing, found %d plan files", len(entries))
	}
}

func TestPlanUnknownDraftFieldWritesNothing(t *testing.T) {
	ws := lifecycleEnv(t)
	scriptModel(t, schema.AssistantMessage(`{"proposed_actions":[],"proposed_files":[],"proposed_commands":[],"required_capabilities":[],"acceptance_checks":["go test"],"risks":[],"assumptions":[],"questions":[],"surprise":"x"}`, nil))
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"plan", "-workspace", ws, "do a thing"}, strings.NewReader(""), &stdout, &stderr); code == 0 {
		t.Fatal("unknown draft field must fail strict parsing")
	}
	if entries, err := os.ReadDir(PlansDir()); err == nil && len(entries) > 0 {
		t.Error("unknown-field draft must write nothing")
	}
}

func TestPlanUnknownCapabilityRejected(t *testing.T) {
	if _, err := parsePlanDraft(`{"proposed_actions":[],"proposed_files":[],"proposed_commands":[],"required_capabilities":["network"],"acceptance_checks":["go test"],"risks":[],"assumptions":[],"questions":[]}`); !errors.Is(err, ErrPlanDraft) {
		t.Errorf("unknown capability err = %v, want ErrPlanDraft", err)
	}
}

// TestPlanDraftSurvivesThinkBlocks reproduces the v0.1.0-rc.1 soak failure:
// MiniMax-M3 interleaves <think> blocks that may contain braces; extraction
// must ignore them and find the real draft.
func TestPlanDraftSurvivesThinkBlocks(t *testing.T) {
	cases := map[string]string{
		"think with braces before draft": "<think>Types: 1. `EventType` {has one} — plan: {\"pseudo\": true}</think>\n" + goodDraft,
		"think after draft":              goodDraft + "\n<think>double-checking {things}</think>",
		"multiple think blocks":          "<think>first {x}</think>\nHere is the plan:\n" + goodDraft + "\n<think>done</think>",
		"uppercase tag":                  "<THINK>reasoning {a}{b}</THINK>" + goodDraft,
	}
	for name, answer := range cases {
		t.Run(name, func(t *testing.T) {
			d, err := parsePlanDraft(answer)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(d.ProposedFiles) != 1 || d.ProposedFiles[0] != "hello.txt" {
				t.Errorf("draft = %+v", d)
			}
		})
	}
}

// TestPlanDraftPrefersTheRealObjectOverStrays: prose objects and trailing
// empty objects must not outrank the draft (a draft needs acceptance checks).
func TestPlanDraftPrefersTheRealObjectOverStrays(t *testing.T) {
	answers := []string{
		"Note that config uses {\"key\": \"value\"} shapes.\n" + goodDraft,
		goodDraft + "\n{}",
		"{\"unrelated\": 1}\n" + goodDraft + "\nSee {} above.",
	}
	for i, answer := range answers {
		d, err := parsePlanDraft(answer)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if len(d.AcceptanceChecks) == 0 || d.ProposedFiles[0] != "hello.txt" {
			t.Errorf("case %d picked the wrong object: %+v", i, d)
		}
	}
}

// TestPlanDraftSurvivesUnbalancedProseQuotesAndBraces: PR #7 review finding —
// prose BEFORE the draft containing an unbalanced quoted brace must not
// corrupt extraction (decode-driven scanning, not span-guessing).
func TestPlanDraftSurvivesUnbalancedProseQuotesAndBraces(t *testing.T) {
	answers := []string{
		`Some "prose { without closing brace" and then the draft:` + "\n" + goodDraft,
		`Note: config has a stray { here and an unmatched " quote` + "\n" + goodDraft,
	}
	for i, answer := range answers {
		d, err := parsePlanDraft(answer)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if len(d.ProposedFiles) != 1 || d.ProposedFiles[0] != "hello.txt" {
			t.Errorf("case %d draft = %+v", i, d)
		}
	}
}

// TestPlanDraftPreservesLiteralThinkTagsInsideStrings: PR #7 review finding —
// a legitimate draft whose STRING VALUES mention think tags must parse intact
// (raw-first extraction; stripping is only the fallback).
func TestPlanDraftPreservesLiteralThinkTagsInsideStrings(t *testing.T) {
	draft := `{"proposed_actions":["document the <think>reasoning</think> marker handling"],"proposed_files":["hello.txt"],"proposed_commands":[],"required_capabilities":["writes"],"acceptance_checks":["go test ./..."],"risks":[],"assumptions":[],"questions":[]}`
	d, err := parsePlanDraft(draft)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.ProposedActions[0] != "document the <think>reasoning</think> marker handling" {
		t.Errorf("literal think tags were mangled: %+v", d.ProposedActions)
	}
}

// TestPlanDraftBracesInsideJSONStringsDoNotSplitSpans: the candidate scanner
// must be string-aware.
func TestPlanDraftBracesInsideJSONStringsDoNotSplitSpans(t *testing.T) {
	draft := `{"proposed_actions":["fix the {braced} thing"],"proposed_files":["hello.txt"],"proposed_commands":[],"required_capabilities":["writes"],"acceptance_checks":["go test ./..."],"risks":[],"assumptions":[],"questions":["what about \"quoted } braces\"?"]}`
	d, err := parsePlanDraft("preamble\n" + draft)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.ProposedActions[0] != "fix the {braced} thing" {
		t.Errorf("draft = %+v", d)
	}
}

func TestPlanDraftToleratesCodeFence(t *testing.T) {
	d, err := parsePlanDraft("```json\n" + goodDraft + "\n```")
	if err != nil {
		t.Fatalf("fenced draft: %v", err)
	}
	if len(d.ProposedFiles) != 1 || d.ProposedFiles[0] != "hello.txt" {
		t.Errorf("draft = %+v", d)
	}
}

// TestPlanModeCannotWrite: a model that tries write_file during planning
// finds no such tool bound; whatever the loop does, no workspace mutation may
// occur and no half-written state may leak.
func TestPlanModeCannotWrite(t *testing.T) {
	ws := lifecycleEnv(t)
	scriptModel(t,
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "call_1", Type: "function",
			Function: schema.FunctionCall{Name: "write_file", Arguments: `{"path":"evil.txt","content":"x"}`},
		}}),
		schema.AssistantMessage(goodDraft, nil),
	)
	var stdout, stderr bytes.Buffer
	Run([]string{"plan", "-workspace", ws, "try to write"}, strings.NewReader(""), &stdout, &stderr)
	if _, err := os.Stat(filepath.Join(ws, "evil.txt")); !os.IsNotExist(err) {
		t.Fatal("planning mode wrote into the workspace — read-only guarantee broken")
	}
}

func TestPlanForbiddenProposedFileRejected(t *testing.T) {
	ws := lifecycleEnv(t)
	scriptModel(t, schema.AssistantMessage(`{"proposed_actions":[],"proposed_files":[".env"],"proposed_commands":[],"required_capabilities":[],"acceptance_checks":["go test"],"risks":[],"assumptions":[],"questions":[]}`, nil))
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"plan", "-workspace", ws, "touch secrets"}, strings.NewReader(""), &stdout, &stderr); code == 0 {
		t.Fatal("a plan proposing a secret file must be rejected")
	}
	if !strings.Contains(stderr.String(), "nothing saved") {
		t.Errorf("stderr should state nothing was saved:\n%s", stderr.String())
	}
}
