package version

import (
	"strings"
	"testing"
)

// TestBuildMetadataDefaultsAreHonest proves an un-ldflagged build reports
// "unknown" rather than a fabricated commit or date.
func TestBuildMetadataDefaultsAreHonest(t *testing.T) {
	if BuildCommit != "unknown" {
		t.Errorf("BuildCommit default = %q, want \"unknown\" (test binaries build without ldflags)", BuildCommit)
	}
	if BuildDate != "unknown" {
		t.Errorf("BuildDate default = %q, want \"unknown\"", BuildDate)
	}
}

func TestGoVersionReportsToolchain(t *testing.T) {
	if got := GoVersion(); !strings.HasPrefix(got, "go") {
		t.Errorf("GoVersion() = %q, want a go-prefixed toolchain version", got)
	}
}

// TestEinoVersionMatchesPinnedEngine asserts the linked Eino module version is
// the one this package pins as EngineVersion — the constant and go.mod must
// move together.
func TestEinoVersionMatchesPinnedEngine(t *testing.T) {
	got := EinoVersion()
	if got == "" {
		t.Fatal("EinoVersion() is empty")
	}
	if got != EngineVersion {
		t.Errorf("EinoVersion() = %q, want pinned EngineVersion %q (bump the constant with go.mod)", got, EngineVersion)
	}
}

// TestNoBarePersonaInIdentityConstants guards the identity contract at the
// version layer: no flat string constant may be the bare persona.
func TestNoBarePersonaInIdentityConstants(t *testing.T) {
	for name, v := range map[string]string{
		"Agent":     Agent,
		"Component": Component,
		"Runtime":   Runtime,
	} {
		if v == "bob" {
			t.Errorf("%s = %q: the bare persona must never be a machine key", name, v)
		}
	}
}
