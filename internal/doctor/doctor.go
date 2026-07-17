// Package doctor runs Bob's preflight checks: is this environment actually
// able to run the plan/run/verify lifecycle? Every check is deterministic
// given its injected dependencies (environment reader, PATH lookup, git
// state, dialer), reports a stable machine name, and is content-safe — a
// credential check reports presence/absence only and NEVER echoes key
// material.
package doctor

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/intent-solutions-io/iam-bob-eino/internal/config"
	"github.com/intent-solutions-io/iam-bob-eino/internal/gitstate"
	"github.com/intent-solutions-io/iam-bob-eino/internal/provider"
)

// Status is a check outcome.
type Status string

// Check outcomes. SKIPPED is not a failure — it means the check was
// deliberately not run (e.g. network checks without --net).
const (
	StatusPass    Status = "PASS"
	StatusWarn    Status = "WARN"
	StatusFail    Status = "FAIL"
	StatusSkipped Status = "SKIPPED"
)

// Check is one preflight result. Name is a stable machine identifier
// (dot-separated, never renamed casually — scripts key on it).
type Check struct {
	Name     string `json:"name"`
	Status   Status `json:"status"`
	Required bool   `json:"required"`
	Detail   string `json:"detail"`
}

// Options parameterizes Run. Every external dependency is injectable so the
// full check matrix is testable hermetically; zero-value fields fall back to
// the real environment.
type Options struct {
	// Cfg is the merged, validated (or deliberately unvalidated) config.
	Cfg config.Config
	// Network enables the reachability check (doctor --net).
	Network bool
	// Getenv reads environment variables; nil means os.Getenv.
	Getenv func(string) string
	// LookPath resolves binaries; nil means exec.LookPath.
	LookPath func(string) (string, error)
	// GitHead reads git state for a directory; nil means gitstate.Head.
	GitHead func(string) (gitstate.State, error)
	// StateDir is the state directory to probe for writability.
	StateDir string
	// Dial probes network reachability of a host:port; nil means a TCP dial
	// with a short timeout. Only used when Network is true.
	Dial func(hostport string) error
}

// Run executes the full check matrix and returns the results in a stable
// order.
func Run(o Options) []Check {
	if o.Getenv == nil {
		o.Getenv = os.Getenv
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.GitHead == nil {
		o.GitHead = gitstate.Head
	}
	if o.Dial == nil {
		o.Dial = func(hostport string) error {
			conn, err := net.DialTimeout("tcp", hostport, 5*time.Second)
			if err != nil {
				return err
			}
			return conn.Close()
		}
	}

	var out []Check
	add := func(name string, required bool, status Status, detail string) {
		out = append(out, Check{Name: name, Status: status, Required: required, Detail: detail})
	}

	// workspace.path (required): must exist and be a directory.
	wsPath := o.Cfg.Workspace
	if wsPath == "" {
		wsPath = "."
	}
	switch info, err := os.Stat(wsPath); {
	case err != nil:
		add("workspace.path", true, StatusFail, fmt.Sprintf("workspace %q not accessible: %v", wsPath, err))
	case !info.IsDir():
		add("workspace.path", true, StatusFail, fmt.Sprintf("workspace %q is not a directory", wsPath))
	default:
		add("workspace.path", true, StatusPass, fmt.Sprintf("workspace %q exists", wsPath))
	}

	// workspace.git (advisory): plan SHA-pinning needs a repository.
	if st, err := o.GitHead(wsPath); err != nil {
		add("workspace.git", false, StatusWarn, fmt.Sprintf("not a usable git repository (%v); plan SHA-pinning and the variance guard's HEAD check will be skipped", err))
	} else if st.Dirty {
		add("workspace.git", false, StatusWarn, fmt.Sprintf("git repository on %s with uncommitted changes (plans record the dirty start state)", st.Branch))
	} else {
		add("workspace.git", false, StatusPass, fmt.Sprintf("clean git repository on %s", st.Branch))
	}

	// state.dir.writable (required): plans, receipts, and evidence live here.
	add(checkStateDirWritable(o.StateDir))

	// evidence.path.safety (required): the audit trail must resolve outside
	// the workspace so the audited agent cannot reach it through its tools.
	add(checkEvidenceSafety(o.Cfg, o.StateDir, wsPath))

	// provider.selection (required): the provider must be registered and the
	// model non-empty.
	keyEnv, known := provider.KeyEnv(o.Cfg.Provider)
	switch {
	case o.Cfg.Provider == "":
		add("provider.selection", true, StatusFail, "no provider configured")
	case !known:
		add("provider.selection", true, StatusFail, fmt.Sprintf("provider %q is not in the registry", o.Cfg.Provider))
	case o.Cfg.Model == "":
		add("provider.selection", true, StatusFail, fmt.Sprintf("provider %q selected but model is empty", o.Cfg.Provider))
	default:
		add("provider.selection", true, StatusPass, fmt.Sprintf("%s/%s", o.Cfg.Provider, o.Cfg.Model))
	}

	// credential.presence (required): BOOLEAN ONLY — never echo key material.
	switch {
	case !known:
		add("credential.presence", true, StatusFail, "unknown provider; cannot determine credential variable")
	case keyEnv == "":
		add("credential.presence", true, StatusPass, fmt.Sprintf("provider %q requires no credential", o.Cfg.Provider))
	case o.Getenv(keyEnv) == "":
		add("credential.presence", true, StatusFail, fmt.Sprintf("%s is not set (BYOK)", keyEnv))
	default:
		add("credential.presence", true, StatusPass, fmt.Sprintf("%s is set", keyEnv))
	}

	// base_url.valid (required): empty means the registry default; otherwise
	// it must parse as an absolute http(s) URL.
	if o.Cfg.BaseURL == "" {
		add("base_url.valid", true, StatusPass, "using the provider's registry endpoint")
	} else if u, err := url.Parse(o.Cfg.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		add("base_url.valid", true, StatusFail, fmt.Sprintf("base URL %q is not an absolute http(s) URL", o.Cfg.BaseURL))
	} else {
		add("base_url.valid", true, StatusPass, fmt.Sprintf("base URL %q parses", o.Cfg.BaseURL))
	}

	// network.reachability (advisory, opt-in): a TCP dial to the endpoint.
	if !o.Network {
		add("network.reachability", false, StatusSkipped, "network checks disabled (run doctor --net to enable)")
	} else {
		add(checkReachability(o))
	}

	// binary.git (advisory): the lifecycle degrades without it, so WARN.
	if _, err := o.LookPath("git"); err != nil {
		add("binary.git", false, StatusWarn, "git not found on PATH; SHA pinning, the variance guard's HEAD check, and changed-file receipts degrade")
	} else {
		add("binary.git", false, StatusPass, "git found on PATH")
	}

	// binary.rg: deliberately skipped — search_code walks the workspace FS
	// in-process; ripgrep is not used by this runtime.
	add("binary.rg", false, StatusSkipped, "not used by this runtime (search_code is in-process)")

	// policy.capabilities (required): the granted combination must be
	// enforceable (exec without writes is a fiction — a shell can write).
	if o.Cfg.AllowExec && !o.Cfg.AllowWrites {
		add("policy.capabilities", true, StatusFail, "exec granted while writes are denied (contradictory permissions)")
	} else {
		add("policy.capabilities", true, StatusPass, fmt.Sprintf("writes=%v exec=%v", o.Cfg.AllowWrites, o.Cfg.AllowExec))
	}

	// approval.mode (required valid; WARN when auto-approval meets granted
	// capabilities — unattended mutation deserves a visible flag).
	switch o.Cfg.ApprovalMode {
	case "prompt":
		add("approval.mode", true, StatusPass, "interactive approval prompt")
	case "auto":
		if o.Cfg.AllowWrites || o.Cfg.AllowExec {
			add("approval.mode", true, StatusWarn, "auto-approval combined with granted capabilities: in-plan mutations run unattended (plan variance still refuses auto-approval)")
		} else {
			add("approval.mode", true, StatusPass, "auto-approval with no capabilities granted")
		}
	default:
		add("approval.mode", true, StatusFail, fmt.Sprintf("approval mode %q is not \"auto\" or \"prompt\"", o.Cfg.ApprovalMode))
	}

	// limits.bounds (required): steps positive, timeout non-negative.
	if o.Cfg.MaxSteps <= 0 {
		add("limits.bounds", true, StatusFail, fmt.Sprintf("max steps %d must be positive", o.Cfg.MaxSteps))
	} else if o.Cfg.Timeout < 0 {
		add("limits.bounds", true, StatusFail, fmt.Sprintf("timeout %v must be non-negative", o.Cfg.Timeout))
	} else {
		add("limits.bounds", true, StatusPass, fmt.Sprintf("max_steps=%d timeout=%v", o.Cfg.MaxSteps, o.Cfg.Timeout))
	}

	// live_tests.flag (informational): whether the gated live smoke is armed.
	if o.Getenv("INTENT_BOB_EINO_LIVE_SMOKE") == "1" {
		add("live_tests.flag", false, StatusWarn, "INTENT_BOB_EINO_LIVE_SMOKE=1: the live MiniMax smoke test is armed for this environment")
	} else {
		add("live_tests.flag", false, StatusPass, "live smoke disarmed (INTENT_BOB_EINO_LIVE_SMOKE unset)")
	}

	return out
}

// HasRequiredFailure reports whether any required check failed — the doctor
// exit-code criterion.
func HasRequiredFailure(checks []Check) bool {
	for _, c := range checks {
		if c.Required && c.Status == StatusFail {
			return true
		}
	}
	return false
}

func checkStateDirWritable(stateDir string) (string, bool, Status, string) {
	const name = "state.dir.writable"
	if stateDir == "" {
		return name, true, StatusFail, "no state directory resolved"
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return name, true, StatusFail, fmt.Sprintf("cannot create state dir %q: %v", stateDir, err)
	}
	probe := filepath.Join(stateDir, ".doctor-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return name, true, StatusFail, fmt.Sprintf("state dir %q is not writable: %v", stateDir, err)
	}
	_ = os.Remove(probe)
	return name, true, StatusPass, fmt.Sprintf("state dir %q writable", stateDir)
}

func checkEvidenceSafety(cfg config.Config, stateDir, wsPath string) (string, bool, Status, string) {
	const name = "evidence.path.safety"
	evDir := cfg.EvidenceDir
	if evDir == "" {
		evDir = stateDir
	}
	if evDir == "" {
		return name, true, StatusFail, "no evidence directory resolved"
	}
	absEv, err1 := filepath.Abs(evDir)
	absWS, err2 := filepath.Abs(wsPath)
	if err1 != nil || err2 != nil {
		return name, true, StatusFail, "cannot resolve evidence/workspace paths"
	}
	rel, err := filepath.Rel(absWS, absEv)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return name, true, StatusFail, fmt.Sprintf("evidence dir %q resolves inside workspace %q — the agent could rewrite its own audit trail", absEv, absWS)
	}
	return name, true, StatusPass, fmt.Sprintf("evidence dir %q is outside the workspace", absEv)
}

func checkReachability(o Options) (string, bool, Status, string) {
	const name = "network.reachability"
	base := o.Cfg.BaseURL
	if base == "" {
		if registryURL, known := provider.BaseURL(o.Cfg.Provider); known {
			base = registryURL
		}
	}
	if base == "" {
		// The openai provider's registry entry has an empty base URL (SDK
		// default endpoint).
		base = "https://api.openai.com"
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return name, false, StatusFail, fmt.Sprintf("cannot derive an endpoint host from %q", base)
	}
	host := u.Host
	if u.Port() == "" {
		if u.Scheme == "http" {
			host += ":80"
		} else {
			host += ":443"
		}
	}
	if err := o.Dial(host); err != nil {
		return name, false, StatusFail, fmt.Sprintf("endpoint %s unreachable: %v", host, err)
	}
	return name, false, StatusPass, fmt.Sprintf("endpoint %s reachable", host)
}
