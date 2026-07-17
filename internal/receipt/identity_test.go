package receipt

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/identity"
	"github.com/intent-solutions-io/iam-bob-eino/internal/version"
)

func mintIdentity(t *testing.T) *identity.AgentIdentity {
	t.Helper()
	id, err := identity.New(identity.RoleCoding, "local", version.AgentVersion)
	if err != nil {
		t.Fatalf("identity.New: %v", err)
	}
	id = id.WithRun()
	return &id
}

// identityReceipt is sampleReceipt plus the structured machine identity.
func identityReceipt(t *testing.T) RunReceipt {
	r := sampleReceipt()
	r.AgentIdentity = mintIdentity(t)
	return r
}

// TestSchemaVersionIsComponentNamespaced pins the receipt schema id to the
// identity contract: component-namespaced, never the bare persona.
func TestSchemaVersionIsComponentNamespaced(t *testing.T) {
	if !strings.HasPrefix(SchemaVersion, identity.ComponentID+"-receipt/") {
		t.Errorf("SchemaVersion = %q, want %q-receipt/<n>", SchemaVersion, identity.ComponentID)
	}
	if strings.HasPrefix(SchemaVersion, "bob.") || strings.HasPrefix(SchemaVersion, "bob-") {
		t.Errorf("SchemaVersion %q uses the bare persona as a machine key", SchemaVersion)
	}
}

// TestSealedReceiptCarriesIdentityInHash proves the identity object is part
// of the sealed content: it survives round-trip and any post-seal edit to it
// fails VerifyHash.
func TestSealedReceiptCarriesIdentityInHash(t *testing.T) {
	sealed, err := Seal(identityReceipt(t))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if !VerifyHash(sealed) {
		t.Fatal("sealed identity receipt must verify")
	}
	if sealed.AgentIdentity == nil || sealed.AgentIdentity.ComponentID != identity.ComponentID {
		t.Fatalf("sealed receipt lost its identity: %+v", sealed.AgentIdentity)
	}

	// Serialized form carries the contract field name.
	b, _ := json.Marshal(sealed)
	if !strings.Contains(string(b), `"agent_identity"`) {
		t.Fatalf("sealed receipt JSON missing agent_identity: %s", b)
	}

	// Round-trip and re-verify.
	var back RunReceipt
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if !VerifyHash(back) {
		t.Fatal("round-tripped identity receipt fails VerifyHash")
	}

	// Post-seal tamper of any identity field must break the hash.
	forged := *back.AgentIdentity
	forged.RoleID = "exfiltration"
	back.AgentIdentity = &forged
	if VerifyHash(back) {
		t.Fatal("tampered identity must fail VerifyHash")
	}

	// Stripping the identity entirely must also break the hash.
	stripped := sealed
	stripped.AgentIdentity = nil
	if VerifyHash(stripped) {
		t.Fatal("stripped identity must fail VerifyHash")
	}
}

// TestPreIdentityReceiptStillSealsAndVerifies proves the pre-identity shape
// (no agent_identity field) is unaffected: seals, verifies, and emits no
// agent_identity key.
func TestPreIdentityReceiptStillSealsAndVerifies(t *testing.T) {
	sealed, err := Seal(sampleReceipt())
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if !VerifyHash(sealed) {
		t.Fatal("pre-identity receipt must verify")
	}
	b, _ := json.Marshal(sealed)
	if strings.Contains(string(b), "agent_identity") {
		t.Fatalf("pre-identity receipt must not emit agent_identity: %s", b)
	}
}

// TestSealRefusesInvalidIdentity proves an invalid identity cannot be
// laundered into a sealed audit record.
func TestSealRefusesInvalidIdentity(t *testing.T) {
	r := identityReceipt(t)
	forged := *r.AgentIdentity
	forged.ComponentID = "bob" // bare persona as machine key — contract violation
	forged.InstanceID = "bob:local:x1"
	r.AgentIdentity = &forged
	if _, err := Seal(r); !errors.Is(err, identity.ErrBarePersona) {
		t.Fatalf("Seal with bare-persona component = %v, want ErrBarePersona", err)
	}

	r2 := identityReceipt(t)
	forged2 := *r2.AgentIdentity
	forged2.SchemaVersion = "intent-agent-identity/v99"
	r2.AgentIdentity = &forged2
	if _, err := Seal(r2); !errors.Is(err, identity.ErrBadSchemaVersion) {
		t.Fatalf("Seal with unsupported identity schema = %v, want ErrBadSchemaVersion", err)
	}
}

// TestSaveLoadRoundTripsIdentity proves a saved identity receipt loads with
// the identity intact and hash-verified from disk.
func TestSaveLoadRoundTripsIdentity(t *testing.T) {
	sealed, err := Seal(identityReceipt(t))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	path, err := Save(sealed, t.TempDir())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.AgentIdentity == nil || !loaded.AgentIdentity.Equal(*sealed.AgentIdentity) {
		t.Fatal("loaded receipt identity differs from sealed identity")
	}
}
