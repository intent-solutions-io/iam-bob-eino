package governor

import (
	"context"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/approval"
	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
	"github.com/intent-solutions-io/iam-bob-eino/internal/verify"
	"github.com/intent-solutions-io/iam-bob-eino/internal/workspace"
)

// scriptedGuard returns a fixed decision and records what it saw.
type scriptedGuard struct {
	decision GuardDecision
	sawTool  string
}

func (s *scriptedGuard) Check(spec ActionSpec) GuardDecision {
	s.sawTool = spec.Tool
	return s.decision
}

// recordingApprover captures the request and approves/denies as scripted.
type recordingApprover struct {
	last     *approval.Request
	approved bool
}

func (r *recordingApprover) Approve(_ context.Context, req approval.Request) approval.Decision {
	c := req
	r.last = &c
	if r.approved {
		return approval.Decision{Approved: true, ApprovalID: "human:" + req.ActionID}
	}
	return approval.Decision{Approved: false, Reason: "declined"}
}

func guardGov(t *testing.T, guard Guard, appr approval.Approver) (*Governor, *evidence.MemorySink) {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pol := policy.Default()
	pol.AllowWrites = true
	sink := &evidence.MemorySink{}
	g := New(ws, pol, appr, sink)
	g.Guard = guard
	return g, sink
}

func TestGuardDeniedShortCircuitsBeforeApproval(t *testing.T) {
	appr := &recordingApprover{approved: true}
	g, _ := guardGov(t, &scriptedGuard{decision: GuardDecision{Outcome: GuardDenied, Reason: "outside the plan contract"}}, appr)
	spec := ActionSpec{Tool: "write_file", Risk: policy.R3, Asset: "a.txt"}
	tk := g.Begin(spec)
	gate := tk.Authorize(context.Background(), spec)
	tk.FinishDenied(context.Background(), gate.Reason)
	if gate.Allowed {
		t.Fatal("guard denial must block the action")
	}
	if appr.last != nil {
		t.Error("approver must never be consulted after a guard denial")
	}
}

func TestGuardVarianceEscalatesToVarianceApproval(t *testing.T) {
	appr := &recordingApprover{approved: true}
	g, _ := guardGov(t, &scriptedGuard{decision: GuardDecision{Outcome: GuardApprovalRequired, Reason: "unlisted path"}}, appr)
	spec := ActionSpec{Tool: "write_file", Risk: policy.R3, Asset: "a.txt"}
	tk := g.Begin(spec)
	gate := tk.Authorize(context.Background(), spec)
	tk.Finish(context.Background(), "ok", "", verify.NA("test"))
	if !gate.Allowed {
		t.Fatalf("human-approved variance must proceed: %s", gate.Reason)
	}
	if appr.last == nil || !appr.last.Variance {
		t.Fatalf("approval request must carry Variance=true, got %+v", appr.last)
	}
}

func TestGuardVarianceRefusedByAutoApprove(t *testing.T) {
	g, sink := guardGov(t, &scriptedGuard{decision: GuardDecision{Outcome: GuardApprovalRequired, Reason: "unlisted path"}}, approval.AutoApprove{})
	spec := ActionSpec{Tool: "write_file", Risk: policy.R3, Asset: "a.txt"}
	tk := g.Begin(spec)
	gate := tk.Authorize(context.Background(), spec)
	tk.FinishDenied(context.Background(), gate.Reason)
	if gate.Allowed {
		t.Fatal("--yes must not launder a plan variance")
	}
	if len(sink.Records) != 1 || sink.Records[0].Authorization != "denied" {
		t.Errorf("variance refusal must leave a denied evidence record: %+v", sink.Records)
	}
}

func TestGuardAllowKeepsNormalApprovalFlow(t *testing.T) {
	appr := &recordingApprover{approved: true}
	g, _ := guardGov(t, &scriptedGuard{decision: GuardDecision{Outcome: GuardAllow}}, appr)
	spec := ActionSpec{Tool: "write_file", Risk: policy.R3, Asset: "a.txt"}
	tk := g.Begin(spec)
	gate := tk.Authorize(context.Background(), spec)
	tk.Finish(context.Background(), "ok", "", verify.NA("test"))
	if !gate.Allowed {
		t.Fatal("in-plan write must proceed through normal approval")
	}
	if appr.last == nil || appr.last.Variance {
		t.Errorf("in-plan approval must NOT be marked variance: %+v", appr.last)
	}
}

func TestNilGuardIsNoOp(t *testing.T) {
	appr := &recordingApprover{approved: true}
	g, _ := guardGov(t, nil, appr)
	spec := ActionSpec{Tool: "write_file", Risk: policy.R3, Asset: "a.txt"}
	tk := g.Begin(spec)
	if gate := tk.Authorize(context.Background(), spec); !gate.Allowed {
		t.Fatalf("nil guard must not block: %s", gate.Reason)
	}
	tk.Finish(context.Background(), "ok", "", verify.NA("test"))
}

func TestGuardPlanInvalidatedDenies(t *testing.T) {
	g, _ := guardGov(t, &scriptedGuard{decision: GuardDecision{Outcome: GuardPlanInvalidated, Reason: "HEAD moved"}}, approval.AutoApprove{})
	spec := ActionSpec{Tool: "run_command", Risk: policy.R3, Asset: "a.txt"}
	tk := g.Begin(spec)
	gate := tk.Authorize(context.Background(), spec)
	tk.FinishDenied(context.Background(), gate.Reason)
	if gate.Allowed {
		t.Fatal("invalidated plan must block the action")
	}
}
