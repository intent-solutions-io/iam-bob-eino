package planguard

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/governor"
	"github.com/intent-solutions-io/iam-bob-eino/internal/plan"
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
)

const startSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func testPlan() plan.Plan {
	return plan.Plan{
		WorkspaceStartSHA: startSHA,
		ProposedFiles:     []string{"src/main.go", "docs/readme.md"},
		ProposedCommands:  []string{"go build ./..."},
		AcceptanceChecks:  []string{"go test ./..."},
	}
}

func steadyHead() (string, error) { return startSHA, nil }

func spec(risk policy.RiskClass, asset string, assets ...string) governor.ActionSpec {
	return governor.ActionSpec{Tool: "t", Risk: risk, Asset: asset, Assets: assets}
}

func TestReadsAlwaysAllowed(t *testing.T) {
	g := New(testPlan(), steadyHead, nil)
	for _, r := range []policy.RiskClass{policy.R0, policy.R1} {
		if d := g.Check(spec(r, "anything.txt")); d.Outcome != governor.GuardAllow {
			t.Errorf("risk %s: outcome %v, want allow", r, d.Outcome)
		}
	}
}

func TestListedCommandAllowedUnlistedNeedsVarianceApproval(t *testing.T) {
	g := New(testPlan(), steadyHead, nil)
	if d := g.Check(spec(policy.R2, "go test ./...")); d.Outcome != governor.GuardAllow {
		t.Errorf("acceptance-check command: %v (%s), want allow", d.Outcome, d.Reason)
	}
	if d := g.Check(spec(policy.R2, "go  build   ./...")); d.Outcome != governor.GuardAllow {
		t.Errorf("whitespace-variant listed command: %v, want allow (normalized match)", d.Outcome)
	}
	if d := g.Check(spec(policy.R2, "git push origin main")); d.Outcome != governor.GuardApprovalRequired {
		t.Errorf("unlisted command: %v, want approval_required", d.Outcome)
	}
}

func TestListedFileAllowedUnlistedNeedsVarianceApproval(t *testing.T) {
	g := New(testPlan(), steadyHead, nil)
	if d := g.Check(spec(policy.R3, "src/main.go")); d.Outcome != governor.GuardAllow {
		t.Errorf("listed file: %v (%s), want allow", d.Outcome, d.Reason)
	}
	if d := g.Check(spec(policy.R3, "src/other.go")); d.Outcome != governor.GuardApprovalRequired {
		t.Errorf("unlisted file: %v, want approval_required", d.Outcome)
	}
}

func TestMultiAssetPatchJudgedPerPath(t *testing.T) {
	g := New(testPlan(), steadyHead, nil)
	d := g.Check(spec(policy.R3, "src/main.go,docs/readme.md", "src/main.go", "docs/readme.md"))
	if d.Outcome != governor.GuardAllow {
		t.Errorf("all-listed patch: %v, want allow", d.Outcome)
	}
	d = g.Check(spec(policy.R3, "src/main.go,evil.go", "src/main.go", "evil.go"))
	if d.Outcome != governor.GuardApprovalRequired {
		t.Errorf("one-unlisted patch: %v, want approval_required", d.Outcome)
	}
	if !strings.Contains(d.Reason, "evil.go") {
		t.Errorf("reason should name the offending path: %s", d.Reason)
	}
}

func TestBackstopDenialsNeverOfferApproval(t *testing.T) {
	g := New(testPlan(), steadyHead, nil)
	for _, bad := range []string{".git/config", "../outside.txt", ".env", "server.pem", "/abs/path"} {
		if d := g.Check(spec(policy.R3, bad)); d.Outcome != governor.GuardDenied {
			t.Errorf("path %q: %v, want denied (never a variance prompt)", bad, d.Outcome)
		}
	}
}

func TestHeadMovedInvalidatesPlanAndCancelsRun(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	moved := func() (string, error) { return "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", nil }
	g := New(testPlan(), moved, cancel)
	d := g.Check(spec(policy.R3, "src/main.go"))
	if d.Outcome != governor.GuardPlanInvalidated {
		t.Fatalf("moved HEAD: %v, want plan_invalidated", d.Outcome)
	}
	if !errors.Is(context.Cause(ctx), ErrPlanInvalidated) {
		t.Errorf("context cause = %v, want ErrPlanInvalidated", context.Cause(ctx))
	}
}

func TestHeadErrorInvalidatesConservatively(t *testing.T) {
	broken := func() (string, error) { return "", errors.New("git exploded") }
	g := New(testPlan(), broken, nil)
	if d := g.Check(spec(policy.R2, "go test ./...")); d.Outcome != governor.GuardPlanInvalidated {
		t.Errorf("unreadable HEAD: %v, want plan_invalidated (conservative)", d.Outcome)
	}
}

func TestNoStartSHASkipsHeadCheck(t *testing.T) {
	p := testPlan()
	p.WorkspaceStartSHA = ""
	called := false
	g := New(p, func() (string, error) { called = true; return "x", nil }, nil)
	if d := g.Check(spec(policy.R3, "src/main.go")); d.Outcome != governor.GuardAllow {
		t.Errorf("non-git plan: %v, want allow", d.Outcome)
	}
	if called {
		t.Error("headFn must not be consulted when the plan has no start SHA")
	}
}

func TestReadsSkipHeadCheck(t *testing.T) {
	moved := func() (string, error) { return "different", nil }
	g := New(testPlan(), moved, nil)
	if d := g.Check(spec(policy.R0, "a.txt")); d.Outcome != governor.GuardAllow {
		t.Errorf("reads must stay allowed even after HEAD moves: %v", d.Outcome)
	}
}

func TestR4Denied(t *testing.T) {
	g := New(testPlan(), steadyHead, nil)
	if d := g.Check(spec(policy.R4, "anything")); d.Outcome != governor.GuardDenied {
		t.Errorf("R4: %v, want denied", d.Outcome)
	}
}
