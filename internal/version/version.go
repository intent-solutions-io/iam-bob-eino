// Package version centralizes build-identity strings that appear in evidence
// records and CLI output. Keeping them in one place lets the evidence boundary
// report a stable, auditable agent + engine identity.
package version

// These values identify the agent and the underlying Eino engine in every
// evidence record. Engine is pinned to the Eino module version this binary is
// built against (see go.mod); bump both together on dependency upgrades.
const (
	// Agent is the Bob agent identity emitted in evidence records.
	Agent = "iam-bob-eino"

	// Bob is this application's semantic version.
	Bob = "0.1.0"

	// Engine identifies the agent machinery Bob is built on.
	Engine = "cloudwego/eino"

	// EngineVersion is the pinned Eino module version (keep in sync with go.mod).
	EngineVersion = "v0.9.12"
)
