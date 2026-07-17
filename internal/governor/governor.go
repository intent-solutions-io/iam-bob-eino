// Package governor is the single control point every governed tool call passes
// through. It composes the policy boundary, the approval boundary, the AGP
// execution seam, and the evidence boundary into one flow, and it guarantees
// that exactly one content-safe evidence record is emitted for every action —
// whether the action is allowed, denied, executed, or failed.
package governor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/intent-solutions-io/iam-bob-eino/internal/approval"
	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/identity"
	"github.com/intent-solutions-io/iam-bob-eino/internal/limits"
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
	"github.com/intent-solutions-io/iam-bob-eino/internal/seams"
	"github.com/intent-solutions-io/iam-bob-eino/internal/verify"
	"github.com/intent-solutions-io/iam-bob-eino/internal/version"
	"github.com/intent-solutions-io/iam-bob-eino/internal/workspace"
)

// GuardOutcome is a pre-approval Guard's ruling on one action.
type GuardOutcome int

// Guard outcomes. approval_required escalates an otherwise pre-authorized
// action to an explicit VARIANCE approval; denied and plan_invalidated stop
// it outright.
const (
	GuardAllow GuardOutcome = iota
	GuardApprovalRequired
	GuardDenied
	GuardPlanInvalidated
)

// GuardDecision carries a guard outcome plus a content-safe reason.
type GuardDecision struct {
	Outcome GuardOutcome
	Reason  string
}

// Guard is the pre-approval plan hook: it runs AFTER policy and BEFORE
// approval, so it can escalate an in-policy action to a variance approval —
// something the post-approval execution seam structurally cannot express.
type Guard interface {
	Check(spec ActionSpec) GuardDecision
}

// Governor holds the shared governance context injected into every tool.
type Governor struct {
	WS       *workspace.Workspace
	Policy   policy.Policy
	Approver approval.Approver
	Sink     evidence.Sink
	Exec     seams.ExecutionSeam
	Project  seams.EvidenceProjector
	Guard    Guard           // optional plan-variance hook; nil = no plan in force
	Limits   *limits.Tracker // optional per-run usage bounds; nil = unbounded
	Env      string
	Corr     string // correlation id shared by all actions in one run

	// ID is the structured machine identity stamped into every evidence
	// record. Constructed once per run through the identity package (the
	// single creation path); one governor = one running copy = one instance.
	ID identity.AgentIdentity
}

// New builds a Governor with sensible defaults for the local, offline slice.
func New(ws *workspace.Workspace, pol policy.Policy, appr approval.Approver, sink evidence.Sink) *Governor {
	const env = "local"
	id, err := identity.New(identity.RoleCoding, env, version.AgentVersion)
	if err != nil {
		// The constants in identity/version are validated by tests; a failure
		// here means the build itself is inconsistent — refuse to run
		// ungoverned rather than emit evidence with a broken identity.
		panic("governor: invalid agent identity: " + err.Error())
	}
	return &Governor{
		WS:       ws,
		Policy:   pol,
		Approver: appr,
		Sink:     sink,
		Exec:     seams.LocalExecution{},
		Project:  seams.NoopProjector{},
		Env:      env,
		Corr:     newID(),
		ID:       id,
	}
}

// ActionSpec describes an action a tool is about to perform. Asset and Summary
// must be content-safe. Asset is the short subject (workspace-relative path or
// command name) recorded in evidence; Summary is the fuller, faithful
// description shown to a human at the approval prompt (e.g. the full command, or
// a path plus a content hash) so the approver can see what it is authorizing.
type ActionSpec struct {
	Tool    string
	Risk    policy.RiskClass
	Asset   string
	Summary string
	RawArgs string // used only to compute a content-safe args hash

	// Assets optionally lists every touched asset individually (e.g. each
	// file of a multi-file patch) for guards that must judge per path;
	// Asset remains the single display/evidence subject.
	Assets []string
}

// Gate is the authorization outcome returned to the calling tool.
type Gate struct {
	Allowed bool
	Reason  string
}

// Ticket is an in-flight governed action. The tool must call exactly one of
// FinishDenied or Finish; both emit the evidence record. As a backstop, a tool
// should `defer t.EnsureEmitted(ctx)` so a panic between Begin and Finish still
// leaves an evidence record rather than a silent gap.
type Ticket struct {
	g       *Governor
	rec     evidence.Record
	emitted bool
}

// Begin opens a ticket and seeds the evidence record with identity, engine,
// policy, and action metadata.
func (g *Governor) Begin(spec ActionSpec) *Ticket {
	// Copy the governor-held identity so the record owns its own value; the
	// pointer must not alias mutable governor state inside the hash chain.
	agentID := g.ID
	return &Ticket{
		g: g,
		rec: evidence.Record{
			SchemaVersion: version.EvidenceSchemaVersion,
			ActionID:      newID(),
			CorrelationID: g.Corr,
			Timestamp:     evidence.Now(),
			Agent:         evidence.Identity{Name: version.Agent, Version: version.AgentVersion},
			AgentIdentity: &agentID,
			Engine:        version.Engine,
			EngineVersion: version.EngineVersion,
			Tool:          evidence.ToolRef{Name: spec.Tool, Version: version.AgentVersion},
			Asset:         spec.Asset,
			Environment:   g.Env,
			RiskClass:     spec.Risk.String(),
			PolicyVersion: g.Policy.Version,
			PolicyHash:    g.Policy.Hash(),
			ArgsHash:      evidence.HashArgs(spec.RawArgs),
		},
	}
}

// Authorize runs the boundaries in fixed order: limits → policy → guard
// (plan variance) → approval → the AGP execution seam. It records the
// authorization outcome on the ticket and returns whether the action may
// proceed.
func (t *Ticket) Authorize(ctx context.Context, spec ActionSpec) Gate {
	// Usage bounds first: an exhausted run refuses every further action (the
	// denied evidence record is still emitted by the calling tool) and the
	// tracker has already cancelled the run context with the typed cause.
	if t.g.Limits != nil {
		if lerr := t.g.Limits.OnBegin(spec.Tool, t.rec.ArgsHash); lerr != nil {
			t.rec.Authorization = "denied"
			return Gate{Allowed: false, Reason: lerr.Error()}
		}
	}
	dec := t.g.Policy.Evaluate(spec.Risk)
	if !dec.Allowed {
		t.rec.Authorization = "denied"
		return Gate{Allowed: false, Reason: dec.Reason}
	}
	requiresApproval := dec.RequiresApproval
	variance := false
	if t.g.Guard != nil {
		switch gd := t.g.Guard.Check(spec); gd.Outcome {
		case GuardDenied:
			t.rec.Authorization = "denied"
			return Gate{Allowed: false, Reason: "plan guard denied: " + gd.Reason}
		case GuardPlanInvalidated:
			t.rec.Authorization = "denied"
			return Gate{Allowed: false, Reason: "plan invalidated: " + gd.Reason}
		case GuardApprovalRequired:
			requiresApproval = true
			variance = true
		}
	}
	if requiresApproval {
		summary := spec.Summary
		if summary == "" {
			summary = fmt.Sprintf("%s on %s", spec.Tool, spec.Asset)
		}
		ad := t.g.Approver.Approve(ctx, approval.Request{
			ActionID: t.rec.ActionID,
			Tool:     spec.Tool,
			Risk:     spec.Risk,
			Summary:  summary,
			Variance: variance,
		})
		if !ad.Approved {
			t.rec.Authorization = "denied"
			return Gate{Allowed: false, Reason: "not approved: " + ad.Reason}
		}
		t.rec.ApprovalID = ad.ApprovalID
	}
	// Route the action through the AGP-compatible execution seam. Every
	// authorized action passes through here (not just writes) so the governor
	// remains the single control point; the local default is a no-op.
	if err := t.g.Exec.Mediate(ctx, seams.ExecutionRequest{
		ActionID:  t.rec.ActionID,
		Tool:      spec.Tool,
		RiskClass: spec.Risk.String(),
		Summary:   spec.Asset,
	}); err != nil {
		t.rec.Authorization = "denied"
		return Gate{Allowed: false, Reason: "execution seam refused: " + err.Error()}
	}
	t.rec.Authorization = "allowed"
	return Gate{Allowed: true, Reason: dec.Reason}
}

// FinishDenied records a denied/skipped action and emits its evidence. A denied
// finish always means the action was not authorized, so the authorization field
// is stamped here for the pre-authorization rejection paths (e.g. path escape).
func (t *Ticket) FinishDenied(ctx context.Context, reason string) {
	if t.rec.Authorization == "" {
		t.rec.Authorization = "denied"
	}
	t.rec.Execution = "denied"
	t.rec.ExecutionInfo = reason
	t.rec.Verified = verify.StatusNA
	t.emit(ctx)
}

// Finish records an executed action's result and verification, then emits the
// evidence. execution is "ok" or "error".
func (t *Ticket) Finish(ctx context.Context, execution, info string, v verify.Verdict) {
	t.rec.Execution = execution
	t.rec.ExecutionInfo = info
	if execution == "error" {
		t.rec.Verified = verify.StatusUnverified
	} else {
		t.rec.Verified = v.Label()
	}
	t.rec.VerifyInfo = v.Info
	t.emit(ctx)
}

// FinishError records a failed action (could not execute) and emits evidence.
func (t *Ticket) FinishError(ctx context.Context, err error) {
	t.rec.Execution = "error"
	t.rec.Verified = verify.StatusUnverified
	t.rec.Error = err.Error()
	t.emit(ctx)
}

// EnsureEmitted guarantees an evidence record exists for this action even if a
// tool panics or returns without calling a Finish method. Deferred by tools.
func (t *Ticket) EnsureEmitted(ctx context.Context) {
	if t.emitted {
		return
	}
	t.rec.Execution = "aborted"
	t.rec.Verified = verify.StatusUnverified
	if t.rec.Error == "" {
		t.rec.Error = "action aborted before completion (panic or early return)"
	}
	t.emit(ctx)
}

// emit writes the record to the sink and projects it through the MC seam. It is
// idempotent: only the first call for a ticket writes a record.
func (t *Ticket) emit(ctx context.Context) {
	if t.emitted {
		return
	}
	t.emitted = true
	_ = t.g.Sink.Write(t.rec)
	_ = t.g.Project.Project(ctx, t.rec)
	if t.g.Limits != nil {
		t.g.Limits.OnFinish(t.rec.Execution)
	}
}

// newID returns a short random hex identifier for actions and correlations. On
// the (practically impossible) event of an RNG failure it panics rather than
// silently returning colliding all-zero identifiers into the audit trail.
func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("governor: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
