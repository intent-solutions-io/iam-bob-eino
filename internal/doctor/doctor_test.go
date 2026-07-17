package doctor

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/intent-solutions-io/iam-bob-eino/internal/config"
	"github.com/intent-solutions-io/iam-bob-eino/internal/gitstate"
)

// baseOptions returns a fully-healthy hermetic option set; tests mutate one
// dimension at a time.
func baseOptions(t *testing.T) Options {
	t.Helper()
	ws := t.TempDir()
	return Options{
		Cfg: config.Config{
			Provider:     "minimax",
			Model:        "MiniMax-M3",
			Workspace:    ws,
			MaxSteps:     32,
			Timeout:      2 * time.Minute,
			ApprovalMode: "prompt",
		},
		Getenv: func(k string) string {
			if k == "MINIMAX_API_KEY" {
				return "test-credential-value-never-printed"
			}
			return ""
		},
		LookPath: func(string) (string, error) { return "/usr/bin/git", nil },
		GitHead: func(string) (gitstate.State, error) {
			return gitstate.State{HeadSHA: strings.Repeat("a", 40), Branch: "main"}, nil
		},
		StateDir: t.TempDir(),
		Dial:     func(string) error { return nil },
	}
}

func byName(t *testing.T, checks []Check, name string) Check {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("check %q not found in %v", name, checks)
	return Check{}
}

func TestHealthyEnvironmentPassesAllRequired(t *testing.T) {
	checks := Run(baseOptions(t))
	if HasRequiredFailure(checks) {
		t.Errorf("healthy environment reported a required failure: %+v", checks)
	}
	for _, name := range []string{
		"workspace.path", "state.dir.writable", "evidence.path.safety",
		"provider.selection", "credential.presence", "base_url.valid",
		"policy.capabilities", "approval.mode", "limits.bounds",
	} {
		if c := byName(t, checks, name); c.Status != StatusPass || !c.Required {
			t.Errorf("%s = %s required=%v, want PASS required", name, c.Status, c.Required)
		}
	}
}

func TestStableMachineNamesComplete(t *testing.T) {
	checks := Run(baseOptions(t))
	want := []string{
		"workspace.path", "workspace.git", "state.dir.writable",
		"evidence.path.safety", "provider.selection", "credential.presence",
		"base_url.valid", "network.reachability", "binary.git", "binary.rg",
		"policy.capabilities", "approval.mode", "limits.bounds", "live_tests.flag",
	}
	if len(checks) != len(want) {
		t.Fatalf("check count = %d, want %d", len(checks), len(want))
	}
	for i, name := range want {
		if checks[i].Name != name {
			t.Errorf("check[%d] = %q, want %q (stable order)", i, checks[i].Name, name)
		}
	}
}

func TestMissingCredentialFailsBooleanOnly(t *testing.T) {
	o := baseOptions(t)
	o.Getenv = func(string) string { return "" }
	checks := Run(o)
	c := byName(t, checks, "credential.presence")
	if c.Status != StatusFail || !c.Required {
		t.Errorf("missing credential: status=%s required=%v, want required FAIL", c.Status, c.Required)
	}
	if !strings.Contains(c.Detail, "MINIMAX_API_KEY") {
		t.Errorf("detail should name the variable: %q", c.Detail)
	}
	if !HasRequiredFailure(checks) {
		t.Error("missing credential must be a required failure")
	}
}

// TestKeyMaterialNeverPrinted greps the entire rendered check set for the
// sentinel credential value.
func TestKeyMaterialNeverPrinted(t *testing.T) {
	const sentinel = "sk-SENTINEL-NEVER-PRINT-12345"
	o := baseOptions(t)
	o.Getenv = func(k string) string {
		if k == "MINIMAX_API_KEY" {
			return sentinel
		}
		return ""
	}
	for _, c := range Run(o) {
		rendered := fmt.Sprintf("%s %s %s", c.Name, c.Status, c.Detail)
		if strings.Contains(rendered, sentinel) {
			t.Fatalf("check %q leaked key material: %s", c.Name, rendered)
		}
	}
}

func TestNoCredentialProviderPasses(t *testing.T) {
	o := baseOptions(t)
	o.Cfg.Provider, o.Cfg.Model = "ollama", "llama3.1"
	c := byName(t, Run(o), "credential.presence")
	if c.Status != StatusPass {
		t.Errorf("ollama credential.presence = %s, want PASS (no credential required)", c.Status)
	}
}

func TestUnknownProviderFailsSelection(t *testing.T) {
	o := baseOptions(t)
	o.Cfg.Provider = "google"
	checks := Run(o)
	if c := byName(t, checks, "provider.selection"); c.Status != StatusFail {
		t.Errorf("unknown provider selection = %s, want FAIL", c.Status)
	}
	if !HasRequiredFailure(checks) {
		t.Error("unknown provider must be a required failure")
	}
}

func TestNetworkSkippedWithoutOptIn(t *testing.T) {
	o := baseOptions(t)
	o.Network = false
	if c := byName(t, Run(o), "network.reachability"); c.Status != StatusSkipped {
		t.Errorf("network check = %s, want SKIPPED without --net", c.Status)
	}
}

func TestNetworkReachabilityUsesInjectedDialer(t *testing.T) {
	o := baseOptions(t)
	o.Network = true
	var dialed string
	o.Dial = func(hostport string) error { dialed = hostport; return nil }
	if c := byName(t, Run(o), "network.reachability"); c.Status != StatusPass {
		t.Errorf("reachable endpoint = %s, want PASS", c.Status)
	}
	if dialed != "api.minimax.io:443" {
		t.Errorf("dialed %q, want the MiniMax registry endpoint", dialed)
	}
	o.Dial = func(string) error { return errors.New("connection refused") }
	if c := byName(t, Run(o), "network.reachability"); c.Status != StatusFail {
		t.Errorf("unreachable endpoint = %s, want FAIL (advisory)", c.Status)
	}
}

func TestRgSkippedAsUnused(t *testing.T) {
	c := byName(t, Run(baseOptions(t)), "binary.rg")
	if c.Status != StatusSkipped || !strings.Contains(c.Detail, "not used by this runtime") {
		t.Errorf("binary.rg = %s %q, want SKIPPED with the not-used note", c.Status, c.Detail)
	}
}

func TestMissingGitWarnsOnly(t *testing.T) {
	o := baseOptions(t)
	o.LookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	o.GitHead = func(string) (gitstate.State, error) { return gitstate.State{}, gitstate.ErrGitUnavailable }
	checks := Run(o)
	if c := byName(t, checks, "binary.git"); c.Status != StatusWarn {
		t.Errorf("binary.git = %s, want WARN", c.Status)
	}
	if HasRequiredFailure(checks) {
		t.Error("missing git must not be a required failure (lifecycle degrades)")
	}
}

func TestEvidenceInsideWorkspaceFails(t *testing.T) {
	o := baseOptions(t)
	o.Cfg.EvidenceDir = filepath.Join(o.Cfg.Workspace, "evidence")
	if c := byName(t, Run(o), "evidence.path.safety"); c.Status != StatusFail {
		t.Errorf("evidence inside workspace = %s, want FAIL", c.Status)
	}
}

func TestContradictoryCapabilitiesFail(t *testing.T) {
	o := baseOptions(t)
	o.Cfg.AllowExec, o.Cfg.AllowWrites = true, false
	if c := byName(t, Run(o), "policy.capabilities"); c.Status != StatusFail {
		t.Errorf("exec-without-writes = %s, want FAIL", c.Status)
	}
}

func TestAutoApprovalWithCapabilitiesWarns(t *testing.T) {
	o := baseOptions(t)
	o.Cfg.ApprovalMode = "auto"
	o.Cfg.AllowWrites = true
	if c := byName(t, Run(o), "approval.mode"); c.Status != StatusWarn {
		t.Errorf("auto+writes = %s, want WARN", c.Status)
	}
	o.Cfg.AllowWrites = false
	if c := byName(t, Run(o), "approval.mode"); c.Status != StatusPass {
		t.Errorf("auto without capabilities = %s, want PASS", c.Status)
	}
}

func TestLimitsBounds(t *testing.T) {
	o := baseOptions(t)
	o.Cfg.MaxSteps = 0
	if c := byName(t, Run(o), "limits.bounds"); c.Status != StatusFail {
		t.Errorf("zero max steps = %s, want FAIL", c.Status)
	}
}

func TestLiveSmokeFlagReported(t *testing.T) {
	o := baseOptions(t)
	inner := o.Getenv
	o.Getenv = func(k string) string {
		if k == "INTENT_BOB_EINO_LIVE_SMOKE" {
			return "1"
		}
		return inner(k)
	}
	if c := byName(t, Run(o), "live_tests.flag"); c.Status != StatusWarn {
		t.Errorf("armed live smoke = %s, want WARN", c.Status)
	}
}
