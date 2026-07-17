package limits

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestMaxToolCallsTripsOnBudgetPlusOne(t *testing.T) {
	tr := NewTracker(Limits{MaxToolCalls: 3, MaxRepeatedIdentical: 100, MaxConsecutiveFailures: 100}, nil)
	for i := 0; i < 3; i++ {
		if err := tr.OnBegin("read_file", fmt.Sprintf("hash-%d", i)); err != nil {
			t.Fatalf("call %d within budget tripped: %v", i, err)
		}
	}
	err := tr.OnBegin("read_file", "hash-final")
	if err == nil || err.Limit != LimitMaxToolCalls {
		t.Fatalf("call over budget: %v, want max_tool_calls", err)
	}
}

func TestRepeatedIdenticalTripsOnFourthCall(t *testing.T) {
	tr := NewTracker(Default(), nil)
	for i := 0; i < 3; i++ {
		if err := tr.OnBegin("read_file", "same-hash"); err != nil {
			t.Fatalf("identical call %d tripped early: %v", i+1, err)
		}
	}
	err := tr.OnBegin("read_file", "same-hash")
	if err == nil || err.Limit != LimitMaxRepeatedIdentical {
		t.Fatalf("4th identical call: %v, want max_repeated_identical", err)
	}
}

func TestDifferentArgsResetTheIdenticalStreak(t *testing.T) {
	tr := NewTracker(Default(), nil)
	for i := 0; i < 20; i++ {
		hash := "a"
		if i%3 == 2 {
			hash = "b"
		}
		if err := tr.OnBegin("read_file", hash); err != nil {
			t.Fatalf("alternating calls must not trip (call %d): %v", i, err)
		}
	}
}

func TestSameArgsDifferentToolIsNotIdentical(t *testing.T) {
	tr := NewTracker(Default(), nil)
	tools := []string{"read_file", "list_dir"}
	for i := 0; i < 12; i++ {
		if err := tr.OnBegin(tools[i%2], "same-hash"); err != nil {
			t.Fatalf("tool-alternating calls must not trip: %v", err)
		}
	}
}

func TestConsecutiveFailuresTripAndOkResets(t *testing.T) {
	tr := NewTracker(Limits{MaxToolCalls: 100, MaxRepeatedIdentical: 100, MaxConsecutiveFailures: 3}, nil)
	tr.OnFinish("error")
	tr.OnFinish("denied")
	tr.OnFinish("ok") // resets
	tr.OnFinish("error")
	tr.OnFinish("error")
	if tr.Tripped() != nil {
		t.Fatal("tripped before the failure run completed")
	}
	tr.OnFinish("aborted")
	got := tr.Tripped()
	if got == nil || got.Limit != LimitMaxConsecutiveFailures {
		t.Fatalf("Tripped = %v, want max_consecutive_failures", got)
	}
}

func TestTripCancelsRunContextWithTypedCause(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	tr := NewTracker(Limits{MaxToolCalls: 1, MaxRepeatedIdentical: 10, MaxConsecutiveFailures: 10}, cancel)
	if err := tr.OnBegin("t", "h1"); err != nil {
		t.Fatal(err)
	}
	if err := tr.OnBegin("t", "h2"); err == nil {
		t.Fatal("second call must trip")
	}
	var lerr *LimitError
	if cause := context.Cause(ctx); !errors.As(cause, &lerr) || lerr.Limit != LimitMaxToolCalls {
		t.Fatalf("context cause = %v, want *LimitError{max_tool_calls}", cause)
	}
}

func TestTrippedIsTerminalForEveryFurtherCall(t *testing.T) {
	tr := NewTracker(Limits{MaxToolCalls: 1, MaxRepeatedIdentical: 10, MaxConsecutiveFailures: 10}, nil)
	tr.OnBegin("t", "h1")
	first := tr.OnBegin("t", "h2")
	if first == nil {
		t.Fatal("expected trip")
	}
	for i := 0; i < 5; i++ {
		if got := tr.OnBegin("t", fmt.Sprintf("h%d", i)); got != first {
			t.Fatalf("post-trip call %d returned %v, want the same terminal error", i, got)
		}
	}
}

func TestDefaultBounds(t *testing.T) {
	d := Default()
	if d.MaxToolCalls != 64 || d.MaxRepeatedIdentical != 3 || d.MaxConsecutiveFailures != 5 {
		t.Errorf("Default() = %+v, want {64,3,5}", d)
	}
}
