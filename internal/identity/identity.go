// Package identity is the single creation path for Bob's structured machine
// identity. "Bob" is a human-facing persona, not a machine key: used alone it
// collides across binaries, environment variables, services, telemetry, and
// state directories (the audit found real collisions between iam-bob-eino,
// iam-bob-intendant, and iam-bob-pydantic). This package separates the persona
// from a typed, deterministic identity hierarchy so every surface that needs a
// machine name gets an unambiguous one.
//
// No other package may build identity strings by hand — construct an
// AgentIdentity through New and read the exported constants. The identity
// contract is documented in 000-docs/005-DR-STND-bob-eino-identity-contract.md
// and machine-defined in schemas/intent-agent-identity.v1.schema.json.
//
// This is a naming contract only. It is NOT an identity-management, auth, or
// permission system — "Intent Agent Model" is a project family, not IAM.
package identity

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Schema and hierarchy constants. These are the only authoritative values;
// callers must reference them rather than restating the strings.
const (
	// SchemaVersion identifies the identity contract this struct satisfies.
	SchemaVersion = "intent-agent-identity/v1"

	// FamilyID names the model/design family every Bob runtime belongs to.
	FamilyID = "intent-agent-model"

	// PersonaID is the shared human-facing persona. It is never a machine key.
	PersonaID = "bob"

	// AgentID is stable across every compatible Bob runtime.
	AgentID = "intent-agent-model/bob"

	// RuntimeID names the implementation technology of this repo.
	RuntimeID = "eino-go"

	// ImplementationID names the codebase (the repository).
	ImplementationID = "iam-bob-eino"

	// ComponentID is the canonical operational component name used for the
	// binary, service, telemetry service.name, and state paths.
	ComponentID = "intent-bob-eino"

	// RoleCoding is the default role for this runtime's runs.
	RoleCoding = "coding"
)

// Typed sentinel errors returned by Validate, so callers can distinguish
// failure classes without string matching.
var (
	ErrBadSchemaVersion = errors.New("identity: unsupported schema version")
	ErrBadMachineID     = errors.New("identity: machine id must be lowercase kebab-case")
	ErrEmptyField       = errors.New("identity: required field is empty")
	ErrBadInstanceID    = errors.New("identity: instance_id must be component:env:opaque")
	ErrBadPersona       = errors.New("identity: persona_id must be \"bob\"")
	ErrBarePersona      = errors.New("identity: bare persona used as machine key")
	ErrRunEqualInstance = errors.New("identity: run_id must differ from instance_id")
)

// AgentIdentity is the structured machine identity stamped into evidence,
// receipts, telemetry, and CLI output. Field order matters: Canonical()
// serializes in declaration order, and the hash chain binds that byte shape.
type AgentIdentity struct {
	SchemaVersion    string `json:"schema_version"`
	FamilyID         string `json:"family_id"`
	PersonaID        string `json:"persona_id"`
	AgentID          string `json:"agent_id"`
	RuntimeID        string `json:"runtime_id"`
	ImplementationID string `json:"implementation_id"`
	ComponentID      string `json:"component_id"`
	RoleID           string `json:"role_id"`
	InstanceID       string `json:"instance_id"`
	RunID            string `json:"run_id,omitempty"`
	Version          string `json:"version"`
}

// New is the only normal constructor. It mints one running-copy identity for
// this process: role defaults to RoleCoding, env defaults to "local", and the
// instance id is component:env:<opaque> with a crypto/rand suffix. version is
// the application semantic version (internal/version.AgentVersion).
func New(role, env, version string) (AgentIdentity, error) {
	if role == "" {
		role = RoleCoding
	}
	if env == "" {
		env = "local"
	}
	id := AgentIdentity{
		SchemaVersion:    SchemaVersion,
		FamilyID:         FamilyID,
		PersonaID:        PersonaID,
		AgentID:          AgentID,
		RuntimeID:        RuntimeID,
		ImplementationID: ImplementationID,
		ComponentID:      ComponentID,
		RoleID:           role,
		InstanceID:       fmt.Sprintf("%s:%s:%s", ComponentID, env, opaqueID()),
		Version:          version,
	}
	if err := id.Validate(); err != nil {
		return AgentIdentity{}, err
	}
	return id, nil
}

// WithRun returns a copy of the identity carrying a fresh per-operation run id.
func (a AgentIdentity) WithRun() AgentIdentity {
	a.RunID = NewRunID()
	return a
}

// NewRunID mints a per-operation run identifier, distinct in shape from an
// instance id (prefix "run-") so the two can never be confused or equal.
func NewRunID() string {
	return "run-" + opaqueID()
}

// Validate checks the identity against the v1 contract and returns a typed
// sentinel (wrapped with field context) on the first violation.
func (a AgentIdentity) Validate() error {
	if a.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: %q", ErrBadSchemaVersion, a.SchemaVersion)
	}
	// Required, machine-shaped fields (lowercase, no spaces, kebab-case with
	// limited separators).
	machineIDs := []struct{ name, val string }{
		{"family_id", a.FamilyID},
		{"persona_id", a.PersonaID},
		{"agent_id", a.AgentID},
		{"runtime_id", a.RuntimeID},
		{"implementation_id", a.ImplementationID},
		{"component_id", a.ComponentID},
		{"role_id", a.RoleID},
	}
	for _, f := range machineIDs {
		if f.val == "" {
			return fmt.Errorf("%w: %s", ErrEmptyField, f.name)
		}
		if !machineID(f.val) {
			return fmt.Errorf("%w: %s=%q", ErrBadMachineID, f.name, f.val)
		}
	}
	if a.PersonaID != PersonaID {
		return fmt.Errorf("%w: got %q", ErrBadPersona, a.PersonaID)
	}
	// The bare persona must never be a machine key: the component (the name
	// that lands on PATH, in service files, and in telemetry) may not be "bob".
	if a.ComponentID == PersonaID {
		return fmt.Errorf("%w: component_id=%q", ErrBarePersona, a.ComponentID)
	}
	if a.InstanceID == "" {
		return fmt.Errorf("%w: instance_id", ErrEmptyField)
	}
	parts := strings.Split(a.InstanceID, ":")
	if len(parts) != 3 || parts[0] != a.ComponentID || parts[1] == "" || parts[2] == "" {
		return fmt.Errorf("%w: %q", ErrBadInstanceID, a.InstanceID)
	}
	if a.RunID != "" && a.RunID == a.InstanceID {
		return ErrRunEqualInstance
	}
	if a.Version == "" {
		return fmt.Errorf("%w: version", ErrEmptyField)
	}
	return nil
}

// Canonical returns the deterministic JSON serialization of the identity.
// encoding/json emits struct fields in declaration order, so equal identities
// always produce identical bytes — the property the evidence hash chain needs.
func (a AgentIdentity) Canonical() []byte {
	b, err := json.Marshal(a)
	if err != nil {
		// A struct of strings cannot fail to marshal; treat it as fatal.
		panic("identity: canonical marshal failed: " + err.Error())
	}
	return b
}

// Equal reports whether two identities are byte-identical in canonical form.
func (a AgentIdentity) Equal(b AgentIdentity) bool {
	return string(a.Canonical()) == string(b.Canonical())
}

// Display renders the human-facing description: persona first (Bob is for
// humans), machine identity below (the machine keys are for everything else).
func (a AgentIdentity) Display() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Bob — Eino/Go runtime (%s %s)\n", a.ImplementationID, a.Version)
	fmt.Fprintf(&sb, "  component: %s\n", a.ComponentID)
	fmt.Fprintf(&sb, "  agent:     %s\n", a.AgentID)
	fmt.Fprintf(&sb, "  runtime:   %s\n", a.RuntimeID)
	fmt.Fprintf(&sb, "  role:      %s\n", a.RoleID)
	fmt.Fprintf(&sb, "  instance:  %s", a.InstanceID)
	return sb.String()
}

// ResourceAttributes returns the OpenTelemetry resource-attribute contract for
// this identity. It is a naming contract only — no telemetry backend is wired.
// service.name is the component id, never the bare persona, so multiple Bob
// runtimes can never collapse into one "bob" service.
func (a AgentIdentity) ResourceAttributes() map[string]string {
	m := map[string]string{
		"service.namespace":       "intent-solutions",
		"service.name":            a.ComponentID,
		"service.version":         a.Version,
		"service.instance.id":     a.InstanceID,
		"intent.agent.family_id":  a.FamilyID,
		"intent.agent.persona_id": a.PersonaID,
		"intent.agent.agent_id":   a.AgentID,
		"intent.agent.runtime_id": a.RuntimeID,
		"intent.agent.impl_id":    a.ImplementationID,
		"intent.agent.role_id":    a.RoleID,
		"intent.agent.schema":     a.SchemaVersion,
	}
	// A run-bound identity (WithRun) also names its run so per-operation
	// telemetry can correlate with receipts and evidence.
	if a.RunID != "" {
		m["intent.agent.run_id"] = a.RunID
	}
	return m
}

// FamilyMember describes one repo/system in the Bob family for collision
// analysis. ComponentType distinguishes agent runtimes from support systems
// (e.g. Big Brain is a knowledge system, not a runtime).
type FamilyMember struct {
	Repo          string
	RuntimeID     string // empty for non-runtimes
	ComponentID   string
	ComponentType string // "agent-runtime" | "agent-application" | "knowledge-system"
	Binary        string // binary/command name it claims on PATH, if any
	EnvPrefix     string // environment namespace it claims, if any
}

// Collision reports one machine-identity conflict between family members.
type Collision struct {
	Kind    string // "binary" | "env-prefix" | "component"
	Value   string
	Members []string // repos claiming the value
}

// DetectCollisions returns every binary, env-prefix, and component_id claimed
// by more than one family member. It is the validator behind the collision
// matrix: a correct family produces no collisions; the pre-contract family
// produces the two the audit found (binary "bob", env prefix "BOB_").
func DetectCollisions(members []FamilyMember) []Collision {
	var out []Collision
	for _, kind := range []struct {
		name string
		key  func(FamilyMember) string
	}{
		{"binary", func(m FamilyMember) string { return m.Binary }},
		{"env-prefix", func(m FamilyMember) string { return m.EnvPrefix }},
		{"component", func(m FamilyMember) string { return m.ComponentID }},
	} {
		claims := map[string][]string{}
		for _, m := range members {
			v := kind.key(m)
			if v == "" {
				continue
			}
			claims[v] = append(claims[v], m.Repo)
		}
		for v, repos := range claims {
			if len(repos) > 1 {
				out = append(out, Collision{Kind: kind.name, Value: v, Members: repos})
			}
		}
	}
	return out
}

// machineID reports whether s is a valid machine identifier: lowercase ASCII
// letters/digits separated by '-', with '/' allowed between kebab segments
// (for agent_id) — never uppercase, spaces, or leading/trailing separators.
func machineID(s string) bool {
	if s == "" {
		return false
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == "" {
			return false
		}
		if strings.HasPrefix(seg, "-") || strings.HasSuffix(seg, "-") {
			return false
		}
		for _, r := range seg {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			default:
				return false
			}
		}
	}
	return true
}

// opaqueID mints a 26-char lowercase ULID-style identifier: 48 bits of
// millisecond time plus 80 bits of crypto/rand, Crockford-base32 encoded. It
// is opaque — consumers must never parse meaning out of it.
func opaqueID() string {
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], uint64(time.Now().UnixMilli())<<16)
	if _, err := rand.Read(b[6:]); err != nil {
		panic("identity: crypto/rand unavailable: " + err.Error())
	}
	const alphabet = "0123456789abcdefghjkmnpqrstvwxyz"
	var sb strings.Builder
	sb.Grow(26)
	// Encode 128 bits as 26 base32 chars (130 bits capacity; top bits zero).
	var acc uint32
	var bits uint8
	for _, by := range b {
		acc = acc<<8 | uint32(by)
		bits += 8
		for bits >= 5 {
			bits -= 5
			sb.WriteByte(alphabet[(acc>>bits)&31])
		}
	}
	if bits > 0 {
		sb.WriteByte(alphabet[(acc<<(5-bits))&31])
	}
	return sb.String()
}
