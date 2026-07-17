package identity

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/version"
)

// --- construction ---

func TestNewMintsValidIdentity(t *testing.T) {
	id, err := New(RoleCoding, "local", "0.1.0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := id.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// Every hierarchy field carries the exact contract value.
	want := map[string]string{
		"schema_version":    "intent-agent-identity/v1",
		"family_id":         "intent-agent-model",
		"persona_id":        "bob",
		"agent_id":          "intent-agent-model/bob",
		"runtime_id":        "eino-go",
		"implementation_id": "iam-bob-eino",
		"component_id":      "intent-bob-eino",
		"role_id":           "coding",
	}
	got := map[string]string{
		"schema_version":    id.SchemaVersion,
		"family_id":         id.FamilyID,
		"persona_id":        id.PersonaID,
		"agent_id":          id.AgentID,
		"runtime_id":        id.RuntimeID,
		"implementation_id": id.ImplementationID,
		"component_id":      id.ComponentID,
		"role_id":           id.RoleID,
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
}

func TestNewDefaultsRoleAndEnv(t *testing.T) {
	id, err := New("", "", "0.1.0")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if id.RoleID != RoleCoding {
		t.Errorf("default role = %q, want %q", id.RoleID, RoleCoding)
	}
	if !strings.HasPrefix(id.InstanceID, ComponentID+":local:") {
		t.Errorf("instance = %q, want prefix %q", id.InstanceID, ComponentID+":local:")
	}
}

func TestNewRejectsEmptyVersion(t *testing.T) {
	if _, err := New(RoleCoding, "local", ""); !errors.Is(err, ErrEmptyField) {
		t.Fatalf("New with empty version = %v, want ErrEmptyField", err)
	}
}

func TestInstanceIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id, err := New(RoleCoding, "local", "0.1.0")
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if seen[id.InstanceID] {
			t.Fatalf("duplicate instance id %q", id.InstanceID)
		}
		seen[id.InstanceID] = true
	}
}

func TestRunIDsAreUniqueAndDistinctFromInstance(t *testing.T) {
	id, _ := New(RoleCoding, "local", "0.1.0")
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		r := id.WithRun()
		if r.RunID == "" || r.RunID == r.InstanceID {
			t.Fatalf("run id %q must be non-empty and differ from instance %q", r.RunID, r.InstanceID)
		}
		if !strings.HasPrefix(r.RunID, "run-") {
			t.Fatalf("run id %q must carry the run- prefix", r.RunID)
		}
		if seen[r.RunID] {
			t.Fatalf("duplicate run id %q", r.RunID)
		}
		seen[r.RunID] = true
		if err := r.Validate(); err != nil {
			t.Fatalf("Validate with run id: %v", err)
		}
	}
}

// --- validation (table-driven over each field) ---

func valid() AgentIdentity {
	id, err := New(RoleCoding, "local", "0.1.0")
	if err != nil {
		panic(err)
	}
	return id
}

func TestValidateRejections(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*AgentIdentity)
		wantErr error
	}{
		{"unsupported schema version", func(a *AgentIdentity) { a.SchemaVersion = "intent-agent-identity/v9" }, ErrBadSchemaVersion},
		{"empty schema version", func(a *AgentIdentity) { a.SchemaVersion = "" }, ErrBadSchemaVersion},
		{"uppercase family", func(a *AgentIdentity) { a.FamilyID = "Intent-Agent-Model" }, ErrBadMachineID},
		{"space in component", func(a *AgentIdentity) { a.ComponentID = "intent bob eino" }, ErrBadMachineID},
		{"uppercase component", func(a *AgentIdentity) { a.ComponentID = "Intent-Bob-Eino" }, ErrBadMachineID},
		{"empty runtime", func(a *AgentIdentity) { a.RuntimeID = "" }, ErrEmptyField},
		{"empty component", func(a *AgentIdentity) { a.ComponentID = "" }, ErrEmptyField},
		{"empty implementation", func(a *AgentIdentity) { a.ImplementationID = "" }, ErrEmptyField},
		{"empty role", func(a *AgentIdentity) { a.RoleID = "" }, ErrEmptyField},
		{"wrong persona", func(a *AgentIdentity) { a.PersonaID = "alice" }, ErrBadPersona},
		{"bare persona as component", func(a *AgentIdentity) { a.ComponentID = "bob"; a.InstanceID = "bob:local:x1" }, ErrBarePersona},
		{"malformed instance: no separators", func(a *AgentIdentity) { a.InstanceID = "intent-bob-eino" }, ErrBadInstanceID},
		{"malformed instance: wrong prefix", func(a *AgentIdentity) { a.InstanceID = "bob:local:abc123" }, ErrBadInstanceID},
		{"malformed instance: empty env", func(a *AgentIdentity) { a.InstanceID = "intent-bob-eino::abc123" }, ErrBadInstanceID},
		{"malformed instance: empty opaque", func(a *AgentIdentity) { a.InstanceID = "intent-bob-eino:local:" }, ErrBadInstanceID},
		{"empty instance", func(a *AgentIdentity) { a.InstanceID = "" }, ErrEmptyField},
		{"run equals instance", func(a *AgentIdentity) { a.RunID = a.InstanceID }, ErrRunEqualInstance},
		{"empty version", func(a *AgentIdentity) { a.Version = "" }, ErrEmptyField},
		{"trailing hyphen", func(a *AgentIdentity) { a.FamilyID = "intent-agent-model-" }, ErrBadMachineID},
		{"leading slash in agent id", func(a *AgentIdentity) { a.AgentID = "/bob" }, ErrBadMachineID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := valid()
			tc.mutate(&id)
			err := id.Validate()
			if err == nil {
				t.Fatal("Validate = nil, want error")
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// --- serialization / canonical form ---

func TestCanonicalIsDeterministic(t *testing.T) {
	id := valid()
	if string(id.Canonical()) != string(id.Canonical()) {
		t.Fatal("Canonical must be deterministic for the same value")
	}
	var round AgentIdentity
	if err := json.Unmarshal(id.Canonical(), &round); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if !id.Equal(round) {
		t.Fatal("round-tripped identity must Equal the original")
	}
}

func TestEqualDistinguishesInstances(t *testing.T) {
	a, b := valid(), valid()
	if a.Equal(b) {
		t.Fatal("two freshly minted identities must differ (unique instance ids)")
	}
	if !a.Equal(a) {
		t.Fatal("identity must equal itself")
	}
}

func TestJSONFieldNamesMatchContract(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal(valid().WithRun().Canonical(), &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{
		"schema_version", "family_id", "persona_id", "agent_id", "runtime_id",
		"implementation_id", "component_id", "role_id", "instance_id", "run_id", "version",
	} {
		if _, ok := m[k]; !ok {
			t.Errorf("canonical JSON missing field %q", k)
		}
	}
	if len(m) != 11 {
		t.Errorf("canonical JSON has %d fields, want 11", len(m))
	}
}

// TestSchemaEquivalence unmarshals schemas/intent-agent-identity.v1.schema.json
// and asserts the Go struct's JSON matches it: identical required-field set,
// every Go field declared in the schema, and the schema's example valid under
// the Go validator.
func TestSchemaEquivalence(t *testing.T) {
	raw, err := os.ReadFile("../../schemas/intent-agent-identity.v1.schema.json")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var schema struct {
		Required             []string                   `json:"required"`
		Properties           map[string]json.RawMessage `json:"properties"`
		AdditionalProperties bool                       `json:"additionalProperties"`
		Examples             []json.RawMessage          `json:"examples"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parse schema: %v", err)
	}

	// The schema must declare exactly the struct's JSON fields.
	var m map[string]any
	_ = json.Unmarshal(valid().WithRun().Canonical(), &m)
	for k := range m {
		if _, ok := schema.Properties[k]; !ok {
			t.Errorf("struct field %q missing from schema properties", k)
		}
	}
	for k := range schema.Properties {
		if _, ok := m[k]; !ok {
			t.Errorf("schema property %q missing from struct JSON", k)
		}
	}

	// Required set = all fields except the optional run_id.
	wantRequired := map[string]bool{}
	for k := range m {
		if k != "run_id" {
			wantRequired[k] = true
		}
	}
	gotRequired := map[string]bool{}
	for _, k := range schema.Required {
		gotRequired[k] = true
	}
	for k := range wantRequired {
		if !gotRequired[k] {
			t.Errorf("schema required missing %q", k)
		}
	}
	for k := range gotRequired {
		if !wantRequired[k] {
			t.Errorf("schema requires unexpected field %q", k)
		}
	}

	// Unknown-field policy: closed schema.
	if schema.AdditionalProperties {
		t.Error("schema must set additionalProperties=false (closed contract)")
	}

	// The schema's example must pass the Go validator.
	if len(schema.Examples) == 0 {
		t.Fatal("schema must carry at least one example")
	}
	var ex AgentIdentity
	if err := json.Unmarshal(schema.Examples[0], &ex); err != nil {
		t.Fatalf("unmarshal schema example: %v", err)
	}
	if err := ex.Validate(); err != nil {
		t.Errorf("schema example fails Go validation: %v", err)
	}
}

// --- version-package coherence ---

func TestVersionPackageMirrorsIdentityConstants(t *testing.T) {
	if version.IdentitySchemaVersion != SchemaVersion {
		t.Errorf("version.IdentitySchemaVersion = %q, identity.SchemaVersion = %q", version.IdentitySchemaVersion, SchemaVersion)
	}
	if version.Component != ComponentID {
		t.Errorf("version.Component = %q, identity.ComponentID = %q", version.Component, ComponentID)
	}
	if version.Runtime != RuntimeID {
		t.Errorf("version.Runtime = %q, identity.RuntimeID = %q", version.Runtime, RuntimeID)
	}
	if version.Agent != ImplementationID {
		t.Errorf("version.Agent = %q, identity.ImplementationID = %q", version.Agent, ImplementationID)
	}
}

// --- display / telemetry ---

func TestDisplayKeepsPersonaHumanFacing(t *testing.T) {
	d := valid().Display()
	if !strings.Contains(d, "Bob — Eino/Go runtime") {
		t.Errorf("Display missing persona line: %q", d)
	}
	for _, want := range []string{"intent-bob-eino", "intent-agent-model/bob", "eino-go", "coding"} {
		if !strings.Contains(d, want) {
			t.Errorf("Display missing %q: %q", want, d)
		}
	}
}

func TestResourceAttributesNeverBareBob(t *testing.T) {
	attrs := valid().ResourceAttributes()
	if attrs["service.name"] != ComponentID {
		t.Errorf("service.name = %q, want %q", attrs["service.name"], ComponentID)
	}
	if attrs["service.name"] == "bob" {
		t.Error("service.name must never be the bare persona")
	}
	if attrs["service.namespace"] != "intent-solutions" {
		t.Errorf("service.namespace = %q", attrs["service.namespace"])
	}
	for _, k := range []string{
		"intent.agent.family_id", "intent.agent.persona_id", "intent.agent.agent_id",
		"intent.agent.runtime_id", "intent.agent.impl_id", "intent.agent.role_id",
		"service.instance.id", "service.version", "intent.agent.schema",
	} {
		if attrs[k] == "" {
			t.Errorf("ResourceAttributes missing %q", k)
		}
	}
	// A plain (instance-level) identity emits no run attribute; a run-bound
	// identity emits exactly its run id.
	if _, ok := attrs["intent.agent.run_id"]; ok {
		t.Error("instance-level identity must not emit intent.agent.run_id")
	}
	withRun := valid().WithRun()
	runAttrs := withRun.ResourceAttributes()
	if runAttrs["intent.agent.run_id"] != withRun.RunID {
		t.Errorf("run-bound identity: intent.agent.run_id = %q, want %q", runAttrs["intent.agent.run_id"], withRun.RunID)
	}
	// The only attribute allowed to carry bare "bob" is the persona id itself.
	for k, v := range attrs {
		if v == "bob" && k != "intent.agent.persona_id" {
			t.Errorf("attribute %q = bare persona %q", k, v)
		}
	}
}

// --- collision detection (the audit's findings as executable fixtures) ---

// preContractFamily reproduces the machine-identity claims the audit found in
// the wild BEFORE this contract: two repos claiming binary `bob`, two claiming
// env prefix BOB_.
func preContractFamily() []FamilyMember {
	return []FamilyMember{
		{Repo: "iam-bob-eino", RuntimeID: "eino-go", ComponentID: "intent-bob-eino", ComponentType: "agent-runtime", Binary: "bob", EnvPrefix: "BOB_"},
		{Repo: "iam-bob-intendant", ComponentID: "intent-bob-intendant", ComponentType: "agent-application", Binary: "bob"},
		{Repo: "iam-bob-pydantic", RuntimeID: "pydantic-python", ComponentID: "intent-bob-pydantic", ComponentType: "agent-runtime", EnvPrefix: "BOB_"},
	}
}

// contractFamily is the post-contract target state: unique binaries, unique
// env prefixes, unique components.
func contractFamily() []FamilyMember {
	return []FamilyMember{
		{Repo: "iam-bob-eino", RuntimeID: "eino-go", ComponentID: "intent-bob-eino", ComponentType: "agent-runtime", Binary: "bob-eino", EnvPrefix: "INTENT_BOB_EINO_"},
		{Repo: "iam-bob-pydantic", RuntimeID: "pydantic-python", ComponentID: "intent-bob-pydantic", ComponentType: "agent-runtime", EnvPrefix: "INTENT_BOB_PYDANTIC_"},
		{Repo: "iam-bob-adk-python", RuntimeID: "adk-python", ComponentID: "intent-bob-adk-python", ComponentType: "agent-runtime"},
		{Repo: "iam-bob-langgraph", RuntimeID: "langgraph-python", ComponentID: "intent-bob-langgraph", ComponentType: "agent-runtime"},
		{Repo: "iam-bob-intendant", ComponentID: "intent-bob-intendant", ComponentType: "agent-application", Binary: "bob-intendant"},
		{Repo: "bobs-big-brain-umbrella", ComponentID: "intent-bob-big-brain", ComponentType: "knowledge-system"},
	}
}

func TestDetectCollisionsFindsAuditFindings(t *testing.T) {
	cols := DetectCollisions(preContractFamily())
	byKind := map[string]Collision{}
	for _, c := range cols {
		byKind[c.Kind+":"+c.Value] = c
	}
	bin, ok := byKind["binary:bob"]
	if !ok {
		t.Fatal("must detect the binary `bob` collision")
	}
	if len(bin.Members) != 2 {
		t.Errorf("binary bob claimed by %v, want 2 repos", bin.Members)
	}
	env, ok := byKind["env-prefix:BOB_"]
	if !ok {
		t.Fatal("must detect the BOB_ env-prefix collision")
	}
	if len(env.Members) != 2 {
		t.Errorf("BOB_ claimed by %v, want 2 repos", env.Members)
	}
}

func TestContractFamilyHasNoCollisions(t *testing.T) {
	if cols := DetectCollisions(contractFamily()); len(cols) != 0 {
		t.Fatalf("contract family must be collision-free, got %+v", cols)
	}
}

func TestDetectCollisionsDuplicateComponent(t *testing.T) {
	members := []FamilyMember{
		{Repo: "a", ComponentID: "intent-bob-eino"},
		{Repo: "b", ComponentID: "intent-bob-eino"},
	}
	cols := DetectCollisions(members)
	if len(cols) != 1 || cols[0].Kind != "component" {
		t.Fatalf("want one component collision, got %+v", cols)
	}
}

func TestFamilyClassificationGuards(t *testing.T) {
	for _, m := range contractFamily() {
		switch m.Repo {
		case "bobs-big-brain-umbrella":
			// Big Brain is a support system, never an agent runtime.
			if m.ComponentType != "knowledge-system" {
				t.Errorf("big brain component_type = %q, want knowledge-system", m.ComponentType)
			}
			if m.RuntimeID != "" {
				t.Errorf("big brain must not claim a runtime_id, got %q", m.RuntimeID)
			}
		case "iam-bob-intendant":
			// Intendant is a role/application composed on AGP, not a runtime.
			if m.ComponentType != "agent-application" {
				t.Errorf("intendant component_type = %q, want agent-application", m.ComponentType)
			}
			if m.RuntimeID != "" {
				t.Errorf("intendant must not claim a runtime_id, got %q", m.RuntimeID)
			}
		}
	}
}
