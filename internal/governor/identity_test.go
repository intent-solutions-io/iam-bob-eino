package governor

import (
	"context"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/approval"
	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/identity"
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
	"github.com/intent-solutions-io/iam-bob-eino/internal/verify"
	"github.com/intent-solutions-io/iam-bob-eino/internal/version"
)

// TestGovernorHoldsValidIdentity proves New constructs one valid structured
// identity per run, through the identity package's single creation path.
func TestGovernorHoldsValidIdentity(t *testing.T) {
	g, _ := newTestGov(t, false, approval.DenyAll{})
	if err := g.ID.Validate(); err != nil {
		t.Fatalf("governor identity invalid: %v", err)
	}
	if g.ID.ComponentID != identity.ComponentID {
		t.Errorf("component = %q, want %q", g.ID.ComponentID, identity.ComponentID)
	}
	if g.ID.RoleID != identity.RoleCoding {
		t.Errorf("role = %q, want %q", g.ID.RoleID, identity.RoleCoding)
	}
	if g.ID.Version != version.AgentVersion {
		t.Errorf("version = %q, want %q", g.ID.Version, version.AgentVersion)
	}
}

// TestBeginStampsIdentityIntoEvidence proves every record carries the
// governor's structured identity and the emitted chain verifies.
func TestBeginStampsIdentityIntoEvidence(t *testing.T) {
	g, sink := newTestGov(t, false, approval.DenyAll{})
	for _, tool := range []string{"read_file", "list_dir"} {
		tk := g.Begin(ActionSpec{Tool: tool, Risk: policy.R0, Asset: "x"})
		tk.Finish(context.Background(), "ok", "done", verify.NA("n/a"))
	}
	if len(sink.Records) != 2 {
		t.Fatalf("emitted %d records, want 2", len(sink.Records))
	}
	for i, rec := range sink.Records {
		if rec.AgentIdentity == nil {
			t.Fatalf("record %d missing agent_identity", i)
		}
		if !rec.AgentIdentity.Equal(g.ID) {
			t.Errorf("record %d identity differs from governor identity", i)
		}
		// Flat legacy fields stay coherent with the structured identity.
		if rec.Agent.Name != version.Agent || rec.Agent.Version != version.AgentVersion {
			t.Errorf("record %d flat agent = %+v", i, rec.Agent)
		}
	}
	if evidence.VerifyChain(sink.Records) != -1 {
		t.Fatal("identity-stamped chain must verify")
	}
	// One run = one instance: both records share the instance id.
	if sink.Records[0].AgentIdentity.InstanceID != sink.Records[1].AgentIdentity.InstanceID {
		t.Error("records within one run must share the instance id")
	}
}

// TestRecordIdentityDoesNotAliasGovernor proves the record owns a copy: later
// governor-state mutation cannot rewrite history inside the sink.
func TestRecordIdentityDoesNotAliasGovernor(t *testing.T) {
	g, sink := newTestGov(t, false, approval.DenyAll{})
	tk := g.Begin(ActionSpec{Tool: "read_file", Risk: policy.R0, Asset: "x"})
	tk.Finish(context.Background(), "ok", "done", verify.NA("n/a"))
	before := sink.Records[0].AgentIdentity.RoleID
	g.ID.RoleID = "mutated"
	if sink.Records[0].AgentIdentity.RoleID != before {
		t.Fatal("evidence record identity must not alias governor state")
	}
}
