// Package planguard implements the plan-variance guard: the governor's
// pre-approval hook that compares each risky action against the sealed plan
// authorizing the run.
//
// The ruling ladder, by risk class:
//
//   - R0/R1 (reads/search): always allowed — minor variance in exploration is
//     free, plans do not enumerate reads.
//   - R2 (commands): listed in the plan's proposed commands or acceptance
//     checks → allowed (normal approval flow still applies); unlisted →
//     VARIANCE approval required (AutoApprove refuses it, a human may still
//     say yes).
//   - R3 (writes/patches): every touched path listed in proposed files →
//     allowed; any unlisted path → VARIANCE approval required. Git internals,
//     secret material, and traversal are DENIED outright as a backstop (the
//     tools already refuse them; the guard refuses again independently).
//   - R4: denied.
//
// Before any R2/R3 ruling, the guard re-reads the workspace HEAD: if it no
// longer equals the plan's start SHA, the plan no longer describes reality —
// the ruling is plan_invalidated and the whole run context is cancelled with
// ErrPlanInvalidated so the agent loop stops instead of drifting.
package planguard

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/intent-solutions-io/iam-bob-eino/internal/governor"
	"github.com/intent-solutions-io/iam-bob-eino/internal/plan"
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
)

// ErrPlanInvalidated is the cancel cause set when the workspace HEAD moves
// away from the plan's start SHA mid-run.
var ErrPlanInvalidated = errors.New("plan invalidated: workspace HEAD moved since the plan was created")

// HeadFunc reports the current workspace HEAD SHA.
type HeadFunc func() (string, error)

// Guard is a governor.Guard bound to one sealed plan.
type Guard struct {
	startSHA string
	headFn   HeadFunc
	cancel   context.CancelCauseFunc
	files    map[string]bool
	commands map[string]bool
}

var _ governor.Guard = (*Guard)(nil)

// New builds the guard for one run. headFn may be nil (or the plan may carry
// no start SHA) — then the HEAD re-check is skipped, matching a non-git
// workspace. cancel may be nil; when set, plan invalidation cancels the run.
func New(p plan.Plan, headFn HeadFunc, cancel context.CancelCauseFunc) *Guard {
	g := &Guard{
		startSHA: p.WorkspaceStartSHA,
		headFn:   headFn,
		cancel:   cancel,
		files:    map[string]bool{},
		commands: map[string]bool{},
	}
	for _, f := range p.ProposedFiles {
		g.files[filepath.ToSlash(filepath.Clean(f))] = true
	}
	for _, c := range p.ProposedCommands {
		g.commands[normalizeCommand(c)] = true
	}
	for _, c := range p.AcceptanceChecks {
		g.commands[normalizeCommand(c)] = true
	}
	return g
}

// Check implements governor.Guard.
func (g *Guard) Check(spec governor.ActionSpec) governor.GuardDecision {
	switch spec.Risk {
	case policy.R0, policy.R1:
		return governor.GuardDecision{Outcome: governor.GuardAllow, Reason: "read-only action; plans do not enumerate reads"}
	case policy.R2, policy.R3:
		// Backstop denials first: even a human approval must not be offered
		// for these.
		if spec.Risk == policy.R3 {
			for _, p := range g.paths(spec) {
				if reason := forbiddenWrite(p); reason != "" {
					return governor.GuardDecision{Outcome: governor.GuardDenied, Reason: reason}
				}
			}
		}
		if d, invalid := g.headMoved(); invalid {
			return d
		}
		if spec.Risk == policy.R2 {
			if g.commands[normalizeCommand(spec.Asset)] {
				return governor.GuardDecision{Outcome: governor.GuardAllow, Reason: "command listed in the plan"}
			}
			return governor.GuardDecision{Outcome: governor.GuardApprovalRequired,
				Reason: "command is not in the plan's proposed commands or acceptance checks"}
		}
		for _, p := range g.paths(spec) {
			if !g.files[filepath.ToSlash(filepath.Clean(p))] {
				return governor.GuardDecision{Outcome: governor.GuardApprovalRequired,
					Reason: "path " + p + " is not in the plan's proposed files"}
			}
		}
		return governor.GuardDecision{Outcome: governor.GuardAllow, Reason: "all touched paths listed in the plan"}
	default:
		return governor.GuardDecision{Outcome: governor.GuardDenied, Reason: "risk class outside the plan contract"}
	}
}

// headMoved re-checks the workspace HEAD against the plan's start SHA.
func (g *Guard) headMoved() (governor.GuardDecision, bool) {
	if g.startSHA == "" || g.headFn == nil {
		return governor.GuardDecision{}, false
	}
	head, err := g.headFn()
	if err == nil && head == g.startSHA {
		return governor.GuardDecision{}, false
	}
	reason := "workspace HEAD is " + head + ", plan was created at " + g.startSHA
	if err != nil {
		reason = "workspace HEAD unreadable: " + err.Error()
	}
	if g.cancel != nil {
		g.cancel(ErrPlanInvalidated)
	}
	return governor.GuardDecision{Outcome: governor.GuardPlanInvalidated, Reason: reason}, true
}

// paths returns the per-path asset list of an action.
func (g *Guard) paths(spec governor.ActionSpec) []string {
	if len(spec.Assets) > 0 {
		return spec.Assets
	}
	return []string{spec.Asset}
}

// forbiddenWrite reports why a write path may never be approved, or "".
func forbiddenWrite(p string) string {
	norm := filepath.ToSlash(filepath.Clean(p))
	if filepath.IsAbs(p) || norm == ".." || strings.HasPrefix(norm, "../") {
		return "path escapes the workspace: " + p
	}
	if norm == ".git" || strings.HasPrefix(norm, ".git/") || strings.Contains(norm, "/.git/") {
		return "git internals are never writable: " + p
	}
	base := strings.ToLower(norm[strings.LastIndex(norm, "/")+1:])
	if base == ".env" || strings.HasPrefix(base, ".env.") || base == ".netrc" || base == ".npmrc" ||
		base == "credentials" || strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") ||
		strings.HasPrefix(base, "id_rsa") || strings.HasPrefix(base, "id_ed25519") {
		return "secret material is never writable: " + p
	}
	return ""
}

// normalizeCommand collapses whitespace so cosmetic spacing differences do
// not defeat (or fake) a plan match.
func normalizeCommand(c string) string {
	return strings.Join(strings.Fields(c), " ")
}
