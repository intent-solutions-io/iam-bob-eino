// Package runverify is Bob's independent run-level verifier. It NEVER trusts
// the model: no model handle, no provider client, no agent import can reach
// this package — by construction it inspects only observable state (workspace
// identity, git SHAs, changed files, already-run acceptance exit codes, diffs)
// plus the tamper-evident evidence chain, and renders a verdict.
//
// The load-bearing property: an agent claim ("all tests pass") carries ZERO
// weight. A recorded acceptance exit code of 1 fails the run even when every
// claim says success, and a receipt naming file A while the observed change
// set names file B fails the run.
//
// Verification here is deterministic and rerunnable: the same Input always
// produces an identical Verdict.
package runverify

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
)

// Verdict result values, from best to worst. Precedence when multiple
// conditions hold: tampered > workspace_mismatch > failed > inconclusive >
// verified_with_warnings > verified.
const (
	ResultVerified          = "verified"
	ResultVerifiedWarnings  = "verified_with_warnings"
	ResultFailed            = "failed"
	ResultInconclusive      = "inconclusive"
	ResultTampered          = "tampered"
	ResultWorkspaceMismatch = "workspace_mismatch"
)

// Per-check outcome values recorded in Verdict.Checks.
const (
	checkPass    = "pass"
	checkFail    = "fail"
	checkWarn    = "warn"
	checkSkipped = "skipped"
	checkError   = "error"
)

// Named checks in Verdict.Checks.
const (
	CheckEvidenceChain        = "evidence_chain"
	CheckEvidenceCompleteness = "evidence_completeness"
	CheckWorkspaceIdentity    = "workspace_identity"
	CheckGitEndSHA            = "git_end_sha"
	CheckChangedVsPlan        = "changed_vs_plan"
	CheckForbiddenPaths       = "forbidden_paths"
	CheckAcceptance           = "acceptance"
	CheckSecretScan           = "secret_scan"
)

// Verdict is the verifier's independent judgment of a completed run.
type Verdict struct {
	// Result is one of the Result* constants.
	Result string
	// Warnings are non-fatal observations (e.g. a planned file left unchanged).
	Warnings []string
	// Failures explain why the run did not verify cleanly.
	Failures []string
	// Checks maps each named check to its outcome (pass/fail/warn/skipped/error).
	Checks map[string]string
}

// GitStateFunc reports the observed git HEAD SHA of the workspace. Injecting a
// func (instead of the verifier shelling out) keeps Verify pure and lets tests
// supply state without a real repository.
type GitStateFunc func() (headSHA string, err error)

// ChangedFilesFunc reports the observed set of changed workspace-relative
// paths. When set it overrides Input.ChangedFiles.
type ChangedFilesFunc func() ([]string, error)

// Plan is the minimal, model-free slice of a run plan the verifier needs. It
// is a *claim* about intent, checked against observed state — never trusted.
type Plan struct {
	// WorkspaceRoot is the workspace path the plan/receipt says the run
	// executed in.
	WorkspaceRoot string
	// ProposedFiles are the workspace-relative paths the plan said it would
	// change.
	ProposedFiles []string
	// StartSHA and EndSHA are the git SHAs the receipt recorded at run start
	// and end (may be empty when the workspace is not a git repo).
	StartSHA string
	EndSHA   string
}

// Input carries everything Verify inspects. Every field is observable
// filesystem/git/evidence state or an already-recorded result — there is no
// model handle and no way to pass one.
type Input struct {
	// WorkspaceRoot is the observed absolute workspace path being verified.
	WorkspaceRoot string

	// Plan is the run's recorded intent (untrusted claim).
	Plan Plan

	// Evidence is the run's evidence log; its hash chain is recomputed here.
	Evidence []evidence.Record

	// ExpectedActionIDs, when non-empty, lists evidence ActionIDs that must be
	// present for the run to be complete.
	ExpectedActionIDs []string

	// ChangedFiles is the observed set of changed workspace-relative paths.
	// Ignored when ChangedFilesFn is set.
	ChangedFiles []string
	// ChangedFilesFn, when set, is queried for the observed changed set.
	ChangedFilesFn ChangedFilesFunc

	// GitState, when set, is queried for the observed HEAD SHA, which must
	// match Plan.EndSHA. Nil skips the git check (non-repo workspaces).
	GitState GitStateFunc

	// Acceptance maps each already-run acceptance check name to its recorded
	// exit code. The verifier never runs checks itself; a non-zero exit code
	// fails the run regardless of any claim.
	Acceptance map[string]int
	// RequiredChecks lists acceptance check names that must be present in
	// Acceptance; a missing one renders the run inconclusive.
	RequiredChecks []string

	// Diffs maps changed-file path -> diff or content, scanned for secrets.
	// Matched secret text is never echoed into the Verdict.
	Diffs map[string]string

	// AgentClaim is whatever the agent asserted about the run ("success",
	// "all tests pass", ...). It is recorded for context only and has zero
	// influence on the verdict.
	AgentClaim string
}

// forbiddenPathPatterns match changed paths that a run must never touch:
// git internals and well-known secret material files.
var forbiddenPathPatterns = []struct {
	name  string
	match func(p string) bool
}{
	{"git internals (.git/**)", func(p string) bool {
		return p == ".git" || strings.HasPrefix(p, ".git/") || strings.Contains(p, "/.git/")
	}},
	{"dotenv secret file", func(p string) bool {
		base := path.Base(p)
		return base == ".env" || strings.HasPrefix(base, ".env.")
	}},
	{"private key file", func(p string) bool {
		base := path.Base(p)
		return strings.HasSuffix(base, ".pem") ||
			base == "id_rsa" || base == "id_ed25519" || base == "id_ecdsa" ||
			(strings.HasSuffix(base, ".age") && strings.Contains(base, "key"))
	}},
	{"credentials file", func(p string) bool {
		base := path.Base(p)
		return base == "credentials" || base == "credentials.json" || base == ".netrc" || base == ".npmrc"
	}},
}

// secretPatterns flag canary/secret material inside changed-file diffs. The
// verdict names the pattern and the file, never the matched text.
var secretPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"private key block", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"AWS access key id", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"OpenAI-style secret key", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`)},
	{"GitHub token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)},
	{"generic api key assignment", regexp.MustCompile(`(?i)\b(api[_-]?key|secret[_-]?key|auth[_-]?token)\b\s*[:=]\s*['"][^'"]{8,}['"]`)},
	{"explicit secret canary", regexp.MustCompile(`SECRET_CANARY`)},
}

// Verify independently inspects the run described by in and returns a Verdict.
// It performs no model calls and trusts no claim: every conclusion is derived
// from the evidence chain, workspace/git identity, observed changed files,
// recorded acceptance exit codes, and the provided diffs.
func Verify(in Input) Verdict {
	v := Verdict{Checks: map[string]string{}}
	var tampered, mismatch, failed, inconclusive bool

	fail := func(check, msg string) {
		v.Checks[check] = checkFail
		v.Failures = append(v.Failures, msg)
		failed = true
	}
	warn := func(check, msg string) {
		if v.Checks[check] == "" || v.Checks[check] == checkPass {
			v.Checks[check] = checkWarn
		}
		v.Warnings = append(v.Warnings, msg)
	}

	// 1. Evidence-chain integrity: recompute the PrevHash/RecordHash chain.
	if i := evidence.VerifyChain(in.Evidence); i >= 0 {
		v.Checks[CheckEvidenceChain] = checkFail
		v.Failures = append(v.Failures,
			fmt.Sprintf("evidence chain broken at record %d: hash chain does not recompute", i))
		tampered = true
	} else {
		v.Checks[CheckEvidenceChain] = checkPass
	}

	// 2. Evidence completeness: every expected action id must be present.
	if len(in.ExpectedActionIDs) > 0 {
		present := map[string]bool{}
		for _, rec := range in.Evidence {
			present[rec.ActionID] = true
		}
		var missing []string
		for _, id := range in.ExpectedActionIDs {
			if !present[id] {
				missing = append(missing, id)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			v.Checks[CheckEvidenceCompleteness] = checkError
			v.Failures = append(v.Failures,
				fmt.Sprintf("evidence incomplete: missing records for action ids %s", strings.Join(missing, ", ")))
			inconclusive = true
		} else {
			v.Checks[CheckEvidenceCompleteness] = checkPass
		}
	} else {
		v.Checks[CheckEvidenceCompleteness] = checkSkipped
	}

	// 3. Workspace identity: the receipt's workspace must be the one observed.
	if in.Plan.WorkspaceRoot != "" && in.Plan.WorkspaceRoot != in.WorkspaceRoot {
		v.Checks[CheckWorkspaceIdentity] = checkFail
		v.Failures = append(v.Failures,
			fmt.Sprintf("workspace identity mismatch: plan recorded %q, observed %q",
				in.Plan.WorkspaceRoot, in.WorkspaceRoot))
		mismatch = true
	} else {
		v.Checks[CheckWorkspaceIdentity] = checkPass
	}

	// 4. Git end state: observed HEAD must equal the recorded end SHA.
	switch {
	case in.GitState == nil:
		v.Checks[CheckGitEndSHA] = checkSkipped
	default:
		head, err := in.GitState()
		switch {
		case err != nil:
			v.Checks[CheckGitEndSHA] = checkError
			v.Failures = append(v.Failures, fmt.Sprintf("git state unavailable: %v", err))
			inconclusive = true
		case in.Plan.EndSHA == "":
			warn(CheckGitEndSHA, "plan recorded no end SHA; observed HEAD "+head+" unverifiable against receipt")
		case head != in.Plan.EndSHA:
			fail(CheckGitEndSHA, fmt.Sprintf("git end SHA mismatch: receipt recorded %s, observed HEAD %s", in.Plan.EndSHA, head))
		default:
			v.Checks[CheckGitEndSHA] = checkPass
		}
	}

	// Resolve the observed changed set.
	changed := in.ChangedFiles
	changedKnown := true
	if in.ChangedFilesFn != nil {
		got, err := in.ChangedFilesFn()
		if err != nil {
			v.Checks[CheckChangedVsPlan] = checkError
			v.Checks[CheckForbiddenPaths] = checkError
			v.Failures = append(v.Failures, fmt.Sprintf("changed-file state unavailable: %v", err))
			inconclusive = true
			changedKnown = false
		} else {
			changed = got
		}
	}
	changed = append([]string(nil), changed...)
	sort.Strings(changed)

	if changedKnown {
		// 5. Changed files vs plan: every observed change must be planned.
		planned := map[string]bool{}
		for _, p := range in.Plan.ProposedFiles {
			planned[p] = true
		}
		v.Checks[CheckChangedVsPlan] = checkPass
		for _, f := range changed {
			if !planned[f] {
				fail(CheckChangedVsPlan, fmt.Sprintf("unplanned change: %q was modified but is not in plan.proposed_files", f))
			}
		}
		observed := map[string]bool{}
		for _, f := range changed {
			observed[f] = true
		}
		plannedSorted := append([]string(nil), in.Plan.ProposedFiles...)
		sort.Strings(plannedSorted)
		for _, p := range plannedSorted {
			if !observed[p] {
				warn(CheckChangedVsPlan, fmt.Sprintf("planned file %q shows no observed change", p))
			}
		}

		// 6. Forbidden paths: git internals and secret files must be untouched.
		v.Checks[CheckForbiddenPaths] = checkPass
		for _, f := range changed {
			for _, fp := range forbiddenPathPatterns {
				if fp.match(f) {
					fail(CheckForbiddenPaths, fmt.Sprintf("forbidden path touched: %q (%s)", f, fp.name))
				}
			}
		}
	}

	// 7. Acceptance results: recorded exit codes are the only truth about
	// whether checks passed. Any claim of success is ignored.
	v.Checks[CheckAcceptance] = checkPass
	acceptanceNames := make([]string, 0, len(in.Acceptance))
	for name := range in.Acceptance {
		acceptanceNames = append(acceptanceNames, name)
	}
	sort.Strings(acceptanceNames)
	for _, name := range acceptanceNames {
		if code := in.Acceptance[name]; code != 0 {
			msg := fmt.Sprintf("acceptance check %q exited %d", name, code)
			if in.AgentClaim != "" {
				msg += fmt.Sprintf(" (agent claim %q is contradicted by the recorded exit code and is ignored)", in.AgentClaim)
			}
			fail(CheckAcceptance, msg)
		}
	}
	requiredSorted := append([]string(nil), in.RequiredChecks...)
	sort.Strings(requiredSorted)
	for _, name := range requiredSorted {
		if _, ok := in.Acceptance[name]; !ok {
			if v.Checks[CheckAcceptance] != checkFail {
				v.Checks[CheckAcceptance] = checkError
			}
			v.Failures = append(v.Failures,
				fmt.Sprintf("required acceptance check %q has no recorded result", name))
			inconclusive = true
		}
	}

	// 8. Secret scan over changed-file diffs. Matched text is never echoed.
	v.Checks[CheckSecretScan] = checkPass
	diffPaths := make([]string, 0, len(in.Diffs))
	for p := range in.Diffs {
		diffPaths = append(diffPaths, p)
	}
	sort.Strings(diffPaths)
	for _, p := range diffPaths {
		content := in.Diffs[p]
		for _, sp := range secretPatterns {
			if sp.re.MatchString(content) {
				fail(CheckSecretScan, fmt.Sprintf("secret material in diff of %q: %s detected [REDACTED]", p, sp.name))
			}
		}
	}

	// Final verdict by precedence.
	switch {
	case tampered:
		v.Result = ResultTampered
	case mismatch:
		v.Result = ResultWorkspaceMismatch
	case failed:
		v.Result = ResultFailed
	case inconclusive:
		v.Result = ResultInconclusive
	case len(v.Warnings) > 0:
		v.Result = ResultVerifiedWarnings
	default:
		v.Result = ResultVerified
	}
	return v
}
