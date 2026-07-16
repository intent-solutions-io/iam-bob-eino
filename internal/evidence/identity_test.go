package evidence

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/identity"
)

func mintIdentity(t *testing.T) *identity.AgentIdentity {
	t.Helper()
	id, err := identity.New(identity.RoleCoding, "local", "0.1.0")
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	return &id
}

// TestRecordCarriesStructuredIdentity proves a v2 record embeds the full
// agent_identity object and the chain still verifies.
func TestRecordCarriesStructuredIdentity(t *testing.T) {
	sink := &MemorySink{}
	rec := Record{ActionID: "a1", Agent: Identity{Name: "iam-bob-eino", Version: "0.1.0"}, AgentIdentity: mintIdentity(t)}
	if err := sink.Write(rec); err != nil {
		t.Fatal(err)
	}
	got := sink.Records[0]
	if got.AgentIdentity == nil {
		t.Fatal("written record lost its agent_identity")
	}
	if got.AgentIdentity.ComponentID != identity.ComponentID {
		t.Errorf("component_id = %q, want %q", got.AgentIdentity.ComponentID, identity.ComponentID)
	}
	if VerifyChain(sink.Records) != -1 {
		t.Fatal("chain with identity must verify")
	}
	// The serialized form uses the contract's field name.
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"agent_identity":{"schema_version":"intent-agent-identity/v1"`) {
		t.Errorf("serialized record missing structured identity: %s", b)
	}
}

// TestLegacyRecordStillParsesAndVerifies proves a v1 record (no agent_identity
// field) round-trips and self-verifies: the nil pointer is omitted from JSON,
// reproducing the exact byte shape the original chain hashed.
func TestLegacyRecordStillParsesAndVerifies(t *testing.T) {
	// A chain written by the v1 code path: no AgentIdentity anywhere.
	sink := &MemorySink{}
	for _, id := range []string{"a1", "a2", "a3"} {
		if err := sink.Write(Record{ActionID: id, Agent: Identity{Name: "iam-bob-eino", Version: "0.1.0"}}); err != nil {
			t.Fatal(err)
		}
	}
	// Serialize like the JSONL log, re-parse with the v2 struct.
	var reparsed []Record
	for _, rec := range sink.Records {
		b, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "agent_identity") {
			t.Fatalf("legacy record must not emit agent_identity: %s", b)
		}
		var back Record
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatalf("legacy record failed to parse under v2 struct: %v", err)
		}
		if back.AgentIdentity != nil {
			t.Fatal("legacy record must keep a nil AgentIdentity")
		}
		reparsed = append(reparsed, back)
	}
	if i := VerifyChain(reparsed); i != -1 {
		t.Fatalf("legacy chain broke at %d after v2 re-parse", i)
	}
}

// TestTamperedIdentityBreaksChain proves the identity participates in the
// hash: editing any identity field after the fact invalidates the record.
func TestTamperedIdentityBreaksChain(t *testing.T) {
	sink := &MemorySink{}
	if err := sink.Write(Record{ActionID: "a1", AgentIdentity: mintIdentity(t)}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Write(Record{ActionID: "a2", AgentIdentity: mintIdentity(t)}); err != nil {
		t.Fatal(err)
	}
	if VerifyChain(sink.Records) != -1 {
		t.Fatal("untampered chain must verify")
	}

	// Deep-copy then tamper the first record's identity.
	tampered := make([]Record, len(sink.Records))
	copy(tampered, sink.Records)
	forged := *tampered[0].AgentIdentity
	forged.ComponentID = "intent-bob-impostor"
	tampered[0].AgentIdentity = &forged
	if i := VerifyChain(tampered); i != 0 {
		t.Fatalf("tampered identity must break the chain at 0, got %d", i)
	}

	// Removing the identity entirely must also break it.
	stripped := make([]Record, len(sink.Records))
	copy(stripped, sink.Records)
	stripped[1].AgentIdentity = nil
	if i := VerifyChain(stripped); i != 1 {
		t.Fatalf("stripped identity must break the chain at 1, got %d", i)
	}
}
