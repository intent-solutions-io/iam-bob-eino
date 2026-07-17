// Package version centralizes build-identity strings that appear in evidence
// records and CLI output. Keeping them in one place lets the evidence boundary
// report a stable, auditable agent + engine identity.
//
// The structured identity hierarchy (family/persona/agent/runtime/component)
// lives in internal/identity; this package holds only the version numbers and
// the flat string constants older call sites consume.
package version

import (
	"runtime"
	"runtime/debug"
)

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

// Build metadata injected at link time via -ldflags -X (see the Makefile's
// build target and .goreleaser.yaml). These are vars, not consts, precisely
// so the linker can set them; a plain `go build` leaves the honest defaults
// rather than a fabricated commit or release version.
var (
	// AgentVersion is this application's semantic version. Development
	// builds carry the base version; release builds inject the tag version
	// (e.g. "0.1.0-rc.1") via GoReleaser. It is Bob's OWN version — never
	// the Eino engine version and never a git SHA (BuildCommit holds that,
	// separately).
	AgentVersion = "0.1.0"

	// BuildCommit is the git commit the binary was built from.
	BuildCommit = "unknown"
	// BuildDate is the commit date (ISO 8601) of BuildCommit — the commit
	// date, not the wall clock, so rebuilding the same commit is reproducible.
	BuildDate = "unknown"
)

// GoVersion reports the Go toolchain the binary was compiled with.
func GoVersion() string { return runtime.Version() }

// EinoVersion reports the Eino module version actually linked into the
// binary, read from build info. It falls back to the pinned EngineVersion
// constant when build info is unavailable (e.g. some test binaries).
func EinoVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range bi.Deps {
			if dep.Path == "github.com/cloudwego/eino" {
				return dep.Version
			}
		}
	}
	return EngineVersion
}
