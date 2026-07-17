package cli

// The gated LIVE MiniMax smoke: a real plan → run → verify lifecycle against
// the real provider endpoint. It is DOUBLE-GATED and honest about skipping:
// CI never sets INTENT_BOB_EINO_LIVE_SMOKE, so this test reports "skipped",
// never a live-success claim without a real run. Operators run it via
// scripts/live-smoke.sh.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLiveMiniMaxSmoke(t *testing.T) {
	if os.Getenv("INTENT_BOB_EINO_LIVE_SMOKE") != "1" {
		t.Skip("live smoke disarmed (set INTENT_BOB_EINO_LIVE_SMOKE=1 and MINIMAX_API_KEY; see scripts/live-smoke.sh)")
	}
	apiKey := os.Getenv("MINIMAX_API_KEY")
	if apiKey == "" {
		t.Skip("live smoke armed but skipped: no credential (MINIMAX_API_KEY unset)")
	}

	// Isolate state, keep the credential, pin the documented default model.
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, v := range []string{"INTENT_BOB_EINO_PROVIDER", "INTENT_BOB_EINO_MODEL", "INTENT_BOB_EINO_CONFIG",
		"BOB_PROVIDER", "BOB_MODEL", "BOB_CONFIG", "MINIMAX_BASE_URL", "MINIMAX_MODEL"} {
		t.Setenv(v, "")
	}

	repo := t.TempDir()
	gitRun := func(args ...string) {
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
	gitRun("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "notes.txt"), []byte("release notes draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun("add", ".")
	gitRun("commit", "-q", "-m", "smoke fixture")

	// 1. doctor --net must pass against the real endpoint.
	var so, se bytes.Buffer
	if code := Run([]string{"doctor", "-workspace", repo, "-net"}, strings.NewReader(""), &so, &se); code != 0 {
		t.Fatalf("doctor --net failed:\n%s\n%s", so.String(), se.String())
	}

	// 2. plan against the live model.
	so.Reset()
	se.Reset()
	code := Run([]string{"plan", "-workspace", repo, "-timeout", "5m",
		"append one line reading 'reviewed' to notes.txt; acceptance check: git status"},
		strings.NewReader(""), &so, &se)
	if code != 0 {
		t.Fatalf("live plan failed:\n%s\n%s", so.String(), se.String())
	}
	var planID string
	for _, line := range strings.Split(so.String(), "\n") {
		if strings.HasPrefix(line, "plan_id: ") {
			planID = strings.TrimPrefix(line, "plan_id: ")
		}
	}
	if planID == "" {
		t.Fatalf("no plan_id:\n%s", so.String())
	}

	// 3. run the plan (auto-approving in-plan actions only).
	so.Reset()
	se.Reset()
	code = Run([]string{"run", "-plan", planID, "-workspace", repo,
		"-allow-writes", "-allow-exec", "-yes", "-timeout", "10m"},
		strings.NewReader(""), &so, &se)
	t.Logf("live run output:\n%s\n%s", so.String(), se.String())
	var runID string
	for _, line := range strings.Split(so.String(), "\n") {
		if strings.HasPrefix(line, "run_id: ") {
			runID = strings.TrimPrefix(line, "run_id: ")
		}
	}
	if runID == "" {
		t.Fatalf("no run_id (run did not reach the receipt stage)")
	}
	if code != 0 {
		// A live model may fail acceptance legitimately; the smoke's bar is
		// that the lifecycle machinery held: receipt sealed, chain intact.
		t.Logf("live run exited non-zero; continuing to verify the audit trail held")
	}

	// 4. verify + evidence must both hold on whatever the run recorded.
	so.Reset()
	se.Reset()
	vcode := Run([]string{"verify", "-receipt", runID}, strings.NewReader(""), &so, &se)
	t.Logf("verify:\n%s", so.String())
	so.Reset()
	se.Reset()
	if ccode := Run([]string{"evidence", "verify-chain"}, strings.NewReader(""), &so, &se); ccode != 0 {
		t.Fatalf("evidence chain broken after a live run:\n%s", so.String())
	}
	if code == 0 && vcode != 0 {
		t.Fatalf("run claimed verified but post-hoc verify disagreed")
	}
}
