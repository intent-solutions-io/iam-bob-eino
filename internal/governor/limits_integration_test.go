package governor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/approval"
	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/limits"
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
	"github.com/intent-solutions-io/iam-bob-eino/internal/verify"
	"github.com/intent-solutions-io/iam-bob-eino/internal/workspace"
)

// TestLimitExhaustionDeniesWithEvidenceAndCancel drives real tickets through
// the governor with a 2-call budget and proves: the third call is denied, its
// evidence record is preserved, and the run context is cancelled with the
// typed cause.
func TestLimitExhaustionDeniesWithEvidenceAndCancel(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sink := &evidence.MemorySink{}
	g := New(ws, policy.Default(), approval.DenyAll{}, sink)
	ctx, cancel := context.WithCancelCause(context.Background())
	g.Limits = limits.NewTracker(limits.Limits{MaxToolCalls: 2, MaxRepeatedIdentical: 10, MaxConsecutiveFailures: 10}, cancel)

	specFor := func(i byte) ActionSpec {
		return ActionSpec{Tool: "read_file", Risk: policy.R0, Asset: "a.txt", RawArgs: string([]byte{'x', i})}
	}
	for i := byte(0); i < 2; i++ {
		spec := specFor(i)
		tk := g.Begin(spec)
		if gate := tk.Authorize(ctx, spec); !gate.Allowed {
			t.Fatalf("call %d within budget denied: %s", i, gate.Reason)
		}
		tk.Finish(ctx, "ok", "", verify.NA("test"))
	}

	spec := specFor(9)
	tk := g.Begin(spec)
	gate := tk.Authorize(ctx, spec)
	if gate.Allowed {
		t.Fatal("call over budget must be denied")
	}
	if !strings.Contains(gate.Reason, "max_tool_calls") {
		t.Errorf("denial reason = %q, want the limit name", gate.Reason)
	}
	tk.FinishDenied(ctx, gate.Reason)

	if len(sink.Records) != 3 {
		t.Fatalf("evidence records = %d, want 3 (the denied call is still recorded)", len(sink.Records))
	}
	last := sink.Records[2]
	if last.Authorization != "denied" || last.Execution != "denied" {
		t.Errorf("limit-denied record = %+v", last)
	}
	var lerr *limits.LimitError
	if cause := context.Cause(ctx); !errors.As(cause, &lerr) {
		t.Fatalf("run context cause = %v, want *limits.LimitError", cause)
	}
}
