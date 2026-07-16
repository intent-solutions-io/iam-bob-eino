package governor

import (
	"context"
	"errors"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/approval"
	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
	"github.com/intent-solutions-io/iam-bob-eino/internal/seams"
	"github.com/intent-solutions-io/iam-bob-eino/internal/verify"
	"github.com/intent-solutions-io/iam-bob-eino/internal/workspace"
)

func newTestGov(t *testing.T, allowWrites bool, appr approval.Approver) (*Governor, *evidence.MemorySink) {
	t.Helper()
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pol := policy.Default()
	pol.AllowWrites = allowWrites
	sink := &evidence.MemorySink{}
	return New(ws, pol, appr, sink), sink
}

func TestEmitExactlyOnce(t *testing.T) {
	g, sink := newTestGov(t, false, approval.DenyAll{})
	spec := ActionSpec{Tool: "read_file", Risk: policy.R0, Asset: "x"}
	tk := g.Begin(spec)
	tk.Finish(context.Background(), "ok", "done", verify.NA("n/a"))
	// A stray second terminal call and the deferred backstop must not double-write.
	tk.Finish(context.Background(), "ok", "again", verify.NA("n/a"))
	tk.EnsureEmitted(context.Background())
	if len(sink.Records) != 1 {
		t.Fatalf("emitted %d records, want exactly 1", len(sink.Records))
	}
	if sink.Records[0].Execution != "ok" {
		t.Fatalf("execution = %q, want ok", sink.Records[0].Execution)
	}
}

func TestEnsureEmittedBackstop(t *testing.T) {
	g, sink := newTestGov(t, false, approval.DenyAll{})
	tk := g.Begin(ActionSpec{Tool: "read_file", Risk: policy.R0, Asset: "x"})
	// Simulate a tool that returns/panics before any Finish: only the deferred
	// backstop runs.
	tk.EnsureEmitted(context.Background())
	if len(sink.Records) != 1 || sink.Records[0].Execution != "aborted" {
		t.Fatalf("records = %+v, want one aborted record", sink.Records)
	}
	if sink.Records[0].Error == "" {
		t.Fatal("aborted record must carry an error explanation")
	}
}

func TestAuthorizeDeniesWriteWithoutPolicy(t *testing.T) {
	g, sink := newTestGov(t, false, approval.AutoApprove{}) // writes disabled
	spec := ActionSpec{Tool: "write_file", Risk: policy.R3, Asset: "f"}
	tk := g.Begin(spec)
	gate := tk.Authorize(context.Background(), spec)
	if gate.Allowed {
		t.Fatal("write authorized despite AllowWrites=false")
	}
	tk.FinishDenied(context.Background(), gate.Reason)
	if sink.Records[0].Authorization != "denied" {
		t.Fatalf("authorization = %q, want denied", sink.Records[0].Authorization)
	}
}

func TestAuthorizeAllowsWriteWithApproval(t *testing.T) {
	g, sink := newTestGov(t, true, approval.AutoApprove{}) // writes enabled + approved
	spec := ActionSpec{Tool: "write_file", Risk: policy.R3, Asset: "f", Summary: "write f (10 bytes)"}
	tk := g.Begin(spec)
	gate := tk.Authorize(context.Background(), spec)
	if !gate.Allowed {
		t.Fatalf("write not authorized: %s", gate.Reason)
	}
	tk.Finish(context.Background(), "ok", "wrote", verify.Verdict{Verified: true, Info: "ok"})
	rec := sink.Records[0]
	if rec.Authorization != "allowed" || rec.ApprovalID == "" {
		t.Fatalf("record = %+v, want allowed with approval id", rec)
	}
}

// vetoSeam is an execution seam that refuses everything, to prove the governor
// honors the AGP-compatible seam's veto.
type vetoSeam struct{}

func (vetoSeam) Mediate(context.Context, seams.ExecutionRequest) error {
	return errors.New("seam refused")
}

func TestExecutionSeamCanVeto(t *testing.T) {
	g, sink := newTestGov(t, false, approval.AutoApprove{})
	g.Exec = vetoSeam{}
	spec := ActionSpec{Tool: "run_command", Risk: policy.R2, Asset: "go"}
	tk := g.Begin(spec)
	gate := tk.Authorize(context.Background(), spec)
	if gate.Allowed {
		t.Fatal("action allowed despite execution seam veto")
	}
	tk.FinishDenied(context.Background(), gate.Reason)
	if sink.Records[0].Authorization != "denied" {
		t.Fatalf("authorization = %q, want denied", sink.Records[0].Authorization)
	}
}
