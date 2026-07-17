// Package approval defines the human-in-the-loop authorization boundary for
// actions that policy marks as requiring approval (execution and writes). The
// Approver interface keeps the decision pluggable: automated in tests, denied by
// default in non-interactive runs, and prompt-driven on a terminal.
package approval

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
)

// Request describes an action awaiting approval. All fields are content-safe.
type Request struct {
	ActionID string
	Tool     string
	Risk     policy.RiskClass
	Summary  string
	// Variance marks a PLAN VARIANCE: the action is outside the approved
	// plan and requires a human decision. AutoApprove structurally refuses
	// variance requests — --yes can never launder out-of-plan mutations.
	Variance bool
}

// Decision records whether an action was approved and by what authority.
type Decision struct {
	Approved   bool
	ApprovalID string
	Reason     string
}

// Approver authorizes (or refuses) an action that requires approval.
type Approver interface {
	Approve(ctx context.Context, req Request) Decision
}

// AutoApprove approves every request. Intended for tests and explicit
// --yes runs where the operator has pre-authorized the session.
type AutoApprove struct{}

// Approve implements Approver by approving every IN-PLAN request. A variance
// request is refused: pre-authorization (--yes) covers the approved plan, not
// whatever the model decided to do instead — that always needs a human.
func (AutoApprove) Approve(_ context.Context, req Request) Decision {
	if req.Variance {
		return Decision{Approved: false, Reason: "plan variance requires human approval; --yes cannot authorize out-of-plan actions"}
	}
	return Decision{Approved: true, ApprovalID: "auto:" + req.ActionID, Reason: "auto-approved"}
}

// DenyAll refuses every request. This is the safe default for non-interactive
// runs so unattended sessions cannot execute or mutate without opt-in.
type DenyAll struct{}

// Approve implements Approver by denying unconditionally.
func (DenyAll) Approve(_ context.Context, _ Request) Decision {
	return Decision{Approved: false, Reason: "no approver available (non-interactive); re-run with --yes to authorize"}
}

// Prompt asks a human on a terminal to approve each request. It reads a line
// from In and writes the prompt to Out; a leading 'y' approves.
type Prompt struct {
	In  io.Reader
	Out io.Writer
}

// Approve implements Approver by prompting and reading a yes/no answer. A
// variance request is prefixed with a PLAN VARIANCE banner so the human knows
// this specific action is outside the approved plan.
func (p Prompt) Approve(_ context.Context, req Request) Decision {
	if req.Variance {
		fmt.Fprint(p.Out, "\nPLAN VARIANCE: the following action is NOT in the approved plan.")
	}
	fmt.Fprintf(p.Out, "\n[approval] %s risk=%s action=%s\n  %s\n  approve? [y/N]: ",
		req.Tool, req.Risk, req.ActionID, req.Summary)
	reader := bufio.NewReader(p.In)
	line, _ := reader.ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	if answer == "y" || answer == "yes" {
		return Decision{Approved: true, ApprovalID: "human:" + req.ActionID, Reason: "approved at prompt"}
	}
	return Decision{Approved: false, Reason: "declined at prompt"}
}
