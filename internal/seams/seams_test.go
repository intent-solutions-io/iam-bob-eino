package seams

import (
	"context"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
)

// The local defaults must be safe no-ops so Bob runs fully offline: no knowledge,
// permissive local execution, no evidence projection.
func TestLocalDefaultsAreSafeNoops(t *testing.T) {
	ctx := context.Background()
	if hits, err := (NoopBigBrain{}).Search(ctx, "q", 5); err != nil || hits != nil {
		t.Fatalf("NoopBigBrain = %v,%v want nil,nil", hits, err)
	}
	if err := (LocalExecution{}).Mediate(ctx, ExecutionRequest{Tool: "t"}); err != nil {
		t.Fatalf("LocalExecution.Mediate = %v, want nil", err)
	}
	if err := (NoopProjector{}).Project(ctx, evidence.Record{}); err != nil {
		t.Fatalf("NoopProjector.Project = %v, want nil", err)
	}
}
