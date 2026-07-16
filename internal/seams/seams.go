// Package seams defines the integration boundaries between Bob and the wider
// Intent Solutions estate — Big Brain (governed knowledge), AGP (governed
// execution), and Mission Control (evidence projection). Each is expressed as a
// narrow Go interface with a safe local no-op implementation.
//
// These are deliberately seams, not implementations: Bob is not a knowledge
// database, not an AGP clone, and not a Mission Control clone. When the estate
// wiring is authorized (a decision the owner is taking to HQ), real adapters
// implement these interfaces without touching the rest of the agent.
package seams

import (
	"context"

	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
)

// KnowledgeHit is a single citation-backed result from Big Brain.
type KnowledgeHit struct {
	Citation string
	Snippet  string
	Score    float64
}

// BigBrain is the governed-knowledge seam. The real adapter speaks MCP to the
// governed-brain server (brain_search → qmd:// citations); the local default
// returns nothing so Bob runs fully offline.
type BigBrain interface {
	Search(ctx context.Context, query string, limit int) ([]KnowledgeHit, error)
}

// NoopBigBrain is the offline default: it never returns knowledge.
type NoopBigBrain struct{}

// Search implements BigBrain by returning no hits.
func (NoopBigBrain) Search(_ context.Context, _ string, _ int) ([]KnowledgeHit, error) {
	return nil, nil
}

// ExecutionRequest describes a world-changing action routed through AGP.
type ExecutionRequest struct {
	ActionID  string
	Tool      string
	RiskClass string
	Summary   string
}

// ExecutionSeam is the AGP-compatible execution boundary. The real adapter
// mediates world-changing actions through the agent-governance-plane (policy
// gate → approval → sandboxed exec → signed journal). The local default runs
// in-process; it never fabricates a signature or journal entry.
type ExecutionSeam interface {
	// Mediate reports whether an action may proceed through the execution plane.
	Mediate(ctx context.Context, req ExecutionRequest) error
}

// LocalExecution is the in-process default: it permits actions that already
// passed Bob's own policy/approval boundary, without emitting any AGP journal.
type LocalExecution struct{}

// Mediate implements ExecutionSeam as a permissive local pass-through.
func (LocalExecution) Mediate(_ context.Context, _ ExecutionRequest) error { return nil }

// EvidenceProjector is the Mission-Control projection seam. The real adapter
// projects content-safe evidence records into MC's governance record; the local
// default is a no-op so evidence stays in the append-only local log only.
type EvidenceProjector interface {
	Project(ctx context.Context, rec evidence.Record) error
}

// NoopProjector is the default: evidence is recorded locally but not projected.
type NoopProjector struct{}

// Project implements EvidenceProjector by discarding the record (local-only).
func (NoopProjector) Project(_ context.Context, _ evidence.Record) error { return nil }
