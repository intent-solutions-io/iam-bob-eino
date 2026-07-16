// Package version centralizes build-identity strings that appear in evidence
// records and CLI output. Keeping them in one place lets the evidence boundary
// report a stable, auditable agent + engine identity.
//
// The structured identity hierarchy (family/persona/agent/runtime/component)
// lives in internal/identity; this package holds only the version numbers and
// the flat string constants older call sites consume.
package version

// These values identify the agent and the underlying Eino engine in every
// evidence record. Engine is pinned to the Eino module version this binary is
// built against (see go.mod); bump both together on dependency upgrades.
const (
	// Agent is the implementation id (the codebase) emitted in evidence records.
	Agent = "iam-bob-eino"

	// Component is the canonical operational component name: the binary,
	// service, telemetry service.name, and state-path key. Never bare "bob".
	Component = "intent-bob-eino"

	// Runtime is the implementation technology of this repo.
	Runtime = "eino-go"

	// AgentVersion is this application's semantic version.
	AgentVersion = "0.1.0"

	// Engine identifies the agent machinery Bob is built on.
	Engine = "cloudwego/eino"

	// EngineVersion is the pinned Eino module version (keep in sync with go.mod).
	EngineVersion = "v0.9.12"

	// IdentitySchemaVersion is the machine-identity contract version
	// (mirrors internal/identity.SchemaVersion; asserted equal by test).
	IdentitySchemaVersion = "intent-agent-identity/v1"

	// EvidenceSchemaVersion is the evidence Record contract version. v2 added
	// the structured agent_identity object; v1 records (without it) still
	// parse and self-verify.
	EvidenceSchemaVersion = "intent-bob-eino-evidence/v2"
)
