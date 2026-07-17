package cli

// The gated LIVE MiniMax proof: the real plan → run → verify → evidence
// lifecycle against the real provider endpoint on a DISPOSABLE fixture — a
// minimal Go module with one intentionally failing test that the
// MiniMax-backed agent must correct. It is DOUBLE-GATED and honest about
// skipping: CI never sets INTENT_BOB_EINO_LIVE_SMOKE, so this test reports
// "skipped", never a live-success claim without a real run. Operators run it
// via scripts/live-smoke.sh (procedure: 000-docs/012).

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/receipt"
)

// liveSmokeGate decides whether the live proof may run. Extracted so the
// double gate itself is unit-testable without a credential.
func liveSmokeGate(getenv func(string) string) (run bool, reason string) {
	if getenv("INTENT_BOB_EINO_LIVE_SMOKE") != "1" {
		return false, "live smoke disarmed (set INTENT_BOB_EINO_LIVE_SMOKE=1 and MINIMAX_API_KEY; see scripts/live-smoke.sh)"
	}
	if getenv("MINIMAX_API_KEY") == "" {
		return false, "SKIPPED_NO_CREDENTIAL (live smoke armed but MINIMAX_API_KEY unset)"
	}
	return true, ""
}

func TestLiveSmokeGateRequiresFlagAndCredential(t *testing.T) {
	env := func(m map[string]string) func(string) string {
		return func(k string) string { return m[k] }
	}
	if run, _ := liveSmokeGate(env(map[string]string{})); run {
		t.Error("gate open with neither flag nor credential")
	}
	if run, reason := liveSmokeGate(env(map[string]string{"MINIMAX_API_KEY": "k"})); run || !strings.Contains(reason, "disarmed") {
		t.Errorf("credential without the flag must stay disarmed: %v %q", run, reason)
	}
	if run, reason := liveSmokeGate(env(map[string]string{"INTENT_BOB_EINO_LIVE_SMOKE": "1"})); run || !strings.Contains(reason, "SKIPPED_NO_CREDENTIAL") {
		t.Errorf("flag without credential must record SKIPPED_NO_CREDENTIAL: %v %q", run, reason)
	}
	if run, _ := liveSmokeGate(env(map[string]string{"INTENT_BOB_EINO_LIVE_SMOKE": "1", "MINIMAX_API_KEY": "k"})); !run {
		t.Error("both gates satisfied must run")
	}
}

// liveFixture builds the disposable proof repository: a minimal Go module
// whose single test fails on purpose (Add subtracts). t.TempDir guarantees
// cleanup.
func liveFixture(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	files := map[string]string{
		"go.mod": "module fixture.invalid/mathx\n\ngo 1.25\n",
		"mathx.go": `package mathx

// Add returns the sum of a and b.
func Add(a, b int) int {
	return a - b // intentional defect the agent must correct
}
`,
		"mathx_test.go": `package mathx

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2,3) = %d, want 5", got)
	}
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=smoke", "GIT_AUTHOR_EMAIL=smoke@example.invalid",
			"GIT_COMMITTER_NAME=smoke", "GIT_COMMITTER_EMAIL=smoke@example.invalid",
			"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("add", ".")
	run("commit", "-q", "-m", "fixture with an intentionally failing test")
	if resolved, err := filepath.EvalSymlinks(repo); err == nil {
		repo = resolved
	}
	return repo
}

func TestLiveMiniMaxSmoke(t *testing.T) {
	if run, reason := liveSmokeGate(os.Getenv); !run {
		t.Skip(reason)
	}
	credential := os.Getenv("MINIMAX_API_KEY")

	// Isolate state, keep the credential, pin the documented default model.
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, v := range []string{"INTENT_BOB_EINO_PROVIDER", "INTENT_BOB_EINO_MODEL", "INTENT_BOB_EINO_CONFIG",
		"BOB_PROVIDER", "BOB_MODEL", "BOB_CONFIG", "MINIMAX_BASE_URL", "MINIMAX_MODEL"} {
		t.Setenv(v, "")
	}

	repo := liveFixture(t)
	guardOutput := func(name, s string) {
		t.Helper()
		if strings.Contains(s, credential) {
			t.Fatalf("CREDENTIAL LEAKED into %s output", name)
		}
	}

	// 1. doctor --net against the real endpoint.
	var so, se bytes.Buffer
	if code := Run([]string{"doctor", "-workspace", repo, "-net"}, strings.NewReader(""), &so, &se); code != 0 {
		t.Fatalf("doctor --net failed:\n%s\n%s", so.String(), se.String())
	}
	guardOutput("doctor", so.String()+se.String())

	// 2. plan: the live model must produce a hashed read-only plan for the fix.
	so.Reset()
	se.Reset()
	code := Run([]string{"plan", "-workspace", repo, "-timeout", "5m", "-json",
		"the test in this Go module fails; find the defect in mathx.go and fix it so 'go test ./...' passes. Propose only the minimal change. Acceptance check: go test ./..."},
		strings.NewReader(""), &so, &se)
	guardOutput("plan", so.String()+se.String())
	if code != 0 {
		t.Fatalf("live plan failed:\n%s\n%s", so.String(), se.String())
	}
	var planOut struct {
		PlanID               string   `json:"plan_id"`
		RequiredCapabilities []string `json:"required_capabilities"`
	}
	if err := json.Unmarshal(so.Bytes(), &planOut); err != nil || planOut.PlanID == "" {
		t.Fatalf("plan -json unparseable: %v\n%s", err, so.String())
	}
	t.Logf("LIVE PROOF plan: provider=minimax model=MiniMax-M3 plan_id=%s required_capabilities=%v",
		planOut.PlanID, planOut.RequiredCapabilities)
	for _, capName := range planOut.RequiredCapabilities {
		if capName != "writes" && capName != "exec" {
			t.Fatalf("plan requested an unknown capability %q", capName)
		}
	}

	// 3. run the plan (auto-approval covers in-plan actions only).
	so.Reset()
	se.Reset()
	code = Run([]string{"run", "-plan", planOut.PlanID, "-workspace", repo,
		"-allow-writes", "-allow-exec", "-yes", "-timeout", "10m", "-json"},
		strings.NewReader(""), &so, &se)
	guardOutput("run", so.String()+se.String())
	var runOut struct {
		RunID       string `json:"run_id"`
		Receipt     string `json:"receipt"`
		FinalStatus string `json:"final_status"`
		Verifier    string `json:"verifier"`
	}
	if err := json.Unmarshal(so.Bytes(), &runOut); err != nil || runOut.RunID == "" {
		t.Fatalf("run did not reach the receipt stage (exit %d): %v\n%s\n%s", code, err, so.String(), se.String())
	}
	r, rerr := receipt.Load(runOut.Receipt)
	if rerr != nil {
		t.Fatalf("sealed receipt unloadable: %v", rerr)
	}
	t.Logf("LIVE PROOF run: run_id=%s final_status=%s verifier=%s tool_calls=%d changed_files=%v tests=%v receipt_hash=%s usage=%v",
		r.RunID, r.FinalStatus, r.VerifierResult, r.ToolCalls, r.FilesChanged, r.TestResults, r.ContentHash, r.Usage)
	if code != 0 {
		t.Fatalf("live run exited %d with final_status=%s — the proof requires a verified fix\n%s", code, runOut.FinalStatus, se.String())
	}
	// MiniMax returns usage on every chat completion (verified directly
	// against the API); an empty usage map is a regression in our
	// accounting, not a provider quirk.
	if len(r.Usage) == 0 {
		t.Fatal("receipt carries no provider usage — accounting regression")
	}

	// The correction must be real: the fixture test passes now, and only the
	// expected file changed.
	goTest := exec.Command("go", "test", "./...")
	goTest.Dir = repo
	if out, err := goTest.CombinedOutput(); err != nil {
		t.Fatalf("fixture test still fails after the verified run: %v\n%s", err, out)
	}
	for _, f := range r.FilesChanged {
		if f != "mathx.go" {
			t.Fatalf("unexpected fixture file modified: %q (changed=%v)", f, r.FilesChanged)
		}
	}

	// 4. verify + evidence must both hold post-hoc.
	so.Reset()
	se.Reset()
	if vcode := Run([]string{"verify", "-receipt", r.RunID, "-plan", planOut.PlanID}, strings.NewReader(""), &so, &se); vcode != 0 {
		t.Fatalf("post-hoc verify disagreed with the run:\n%s\n%s", so.String(), se.String())
	}
	guardOutput("verify", so.String()+se.String())
	so.Reset()
	se.Reset()
	if ccode := Run([]string{"evidence", "verify-chain"}, strings.NewReader(""), &so, &se); ccode != 0 {
		t.Fatalf("evidence chain broken after the live run:\n%s", so.String())
	}

	// No credential in the durable artifacts either. Read errors here would
	// silently void the leak check, so they are fatal.
	evRaw, err := os.ReadFile(filepath.Join(StateDir(), "evidence.jsonl"))
	if err != nil {
		t.Fatalf("cannot read the evidence log for the leak check: %v", err)
	}
	rcRaw, err := os.ReadFile(runOut.Receipt)
	if err != nil {
		t.Fatalf("cannot read the receipt for the leak check: %v", err)
	}
	if strings.Contains(string(evRaw), credential) || strings.Contains(string(rcRaw), credential) {
		t.Fatal("CREDENTIAL LEAKED into evidence or receipt")
	}
	t.Logf("LIVE PROOF complete: fixture corrected, receipt sealed + re-verified, chain intact, fixture cleanup via TempDir")
}
