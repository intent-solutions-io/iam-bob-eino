// Package limits enforces per-run usage bounds over governed tool calls: a
// total tool-call budget, a cap on consecutive identical calls (the classic
// stuck-loop signature), and a cap on consecutive failures (the classic
// flailing signature). A tripped limit is terminal for the run: the tracker
// cancels the run context with a typed *LimitError cause, and every
// subsequent call is refused with the same error — there is no auto-continue
// and no retry laundering.
package limits

import (
	"context"
	"fmt"
	"sync"
)

// Limit names, stable identifiers used in typed statuses
// ("limit_exhausted:<name>") and receipts.
const (
	LimitMaxToolCalls           = "max_tool_calls"
	LimitMaxRepeatedIdentical   = "max_repeated_identical"
	LimitMaxConsecutiveFailures = "max_consecutive_failures"
)

// Limits are the per-run bounds.
type Limits struct {
	// MaxToolCalls is the total governed-call budget for one run.
	MaxToolCalls int
	// MaxRepeatedIdentical is the maximum number of CONSECUTIVE calls with an
	// identical (tool, args-hash) signature.
	MaxRepeatedIdentical int
	// MaxConsecutiveFailures is the maximum run of calls finishing without an
	// "ok" execution (errors and denials both count).
	MaxConsecutiveFailures int
}

// Default returns the standard run bounds.
func Default() Limits {
	return Limits{MaxToolCalls: 64, MaxRepeatedIdentical: 3, MaxConsecutiveFailures: 5}
}

// LimitError identifies which bound was exhausted. It is used as the run
// context's cancel cause, so callers classify via errors.As.
type LimitError struct {
	Limit string
	Max   int
}

// Error implements the error interface.
func (e *LimitError) Error() string {
	return fmt.Sprintf("usage limit %s exhausted (max %d)", e.Limit, e.Max)
}

// Tracker counts governed calls for one run. Safe for concurrent use.
type Tracker struct {
	mu      sync.Mutex
	l       Limits
	cancel  context.CancelCauseFunc
	total   int
	lastKey string
	streak  int
	fails   int
	tripped *LimitError
}

// NewTracker builds a tracker over the run's cancel-cause function. cancel
// may be nil (counting still works; the run just is not force-stopped).
func NewTracker(l Limits, cancel context.CancelCauseFunc) *Tracker {
	return &Tracker{l: l, cancel: cancel}
}

// OnBegin is called by the governor as the FIRST authorization step of every
// governed call. It returns the tripping *LimitError when a bound is (or
// already was) exhausted; the caller then denies the action, which still
// leaves a normal denied evidence record.
func (t *Tracker) OnBegin(tool, argsHash string) *LimitError {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tripped != nil {
		return t.tripped
	}
	t.total++
	if t.l.MaxToolCalls > 0 && t.total > t.l.MaxToolCalls {
		return t.trip(LimitMaxToolCalls, t.l.MaxToolCalls)
	}
	key := tool + "\x00" + argsHash
	if key == t.lastKey {
		t.streak++
	} else {
		t.lastKey, t.streak = key, 1
	}
	if t.l.MaxRepeatedIdentical > 0 && t.streak > t.l.MaxRepeatedIdentical {
		return t.trip(LimitMaxRepeatedIdentical, t.l.MaxRepeatedIdentical)
	}
	return nil
}

// OnFinish is called by the governor when a call's evidence is emitted, with
// the recorded execution value ("ok" | "error" | "denied" | "aborted" |
// "skipped"). Anything other than "ok" counts toward the failure run.
func (t *Tracker) OnFinish(execution string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if execution == "ok" {
		t.fails = 0
		return
	}
	t.fails++
	if t.tripped == nil && t.l.MaxConsecutiveFailures > 0 && t.fails >= t.l.MaxConsecutiveFailures {
		t.trip(LimitMaxConsecutiveFailures, t.l.MaxConsecutiveFailures)
	}
}

// Tripped returns the exhausted limit, or nil.
func (t *Tracker) Tripped() *LimitError {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tripped
}

// trip records the terminal limit and cancels the run. Caller holds the lock.
func (t *Tracker) trip(name string, max int) *LimitError {
	t.tripped = &LimitError{Limit: name, Max: max}
	if t.cancel != nil {
		t.cancel(t.tripped)
	}
	return t.tripped
}
