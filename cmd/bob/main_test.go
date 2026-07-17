package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buildBoth compiles the canonical (bob-eino) and legacy (bob) binaries into a
// temp dir and returns their paths. Both entry points are thin wrappers over
// internal/cli — these tests prove the alias cannot drift.
func buildBoth(t *testing.T) (canonical, legacy string) {
	t.Helper()
	dir := t.TempDir()
	canonical = filepath.Join(dir, "bob-eino")
	legacy = filepath.Join(dir, "bob")
	for bin, pkg := range map[string]string{canonical: "../bob-eino", legacy: "."} {
		cmd := exec.Command("go", "build", "-o", bin, pkg)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", pkg, err, out)
		}
	}
	return canonical, legacy
}

// runBin executes a binary and returns stdout and stderr separately.
func runBin(t *testing.T, bin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var so, se strings.Builder
	cmd.Stdout = &so
	cmd.Stderr = &se
	err = cmd.Run()
	return so.String(), se.String(), err
}

// stripInstance drops the per-process instance line so two independently
// launched processes can be compared (each mints a unique instance id).
func stripInstance(s string) string {
	var keep []string
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "instance:") {
			continue
		}
		keep = append(keep, line)
	}
	return strings.Join(keep, "\n")
}

// TestCanonicalAndLegacyShareOneImplementation is the L6 CLI smoke test for
// the dual entry point: identical stdout surface, deprecation only on the
// legacy alias's stderr.
func TestCanonicalAndLegacyShareOneImplementation(t *testing.T) {
	canonical, legacy := buildBoth(t)

	// -version: identity-structured output from both.
	canonOut, canonErr, err := runBin(t, canonical, "-version")
	if err != nil {
		t.Fatalf("bob-eino -version: %v\n%s", err, canonErr)
	}
	legacyOut, legacyErr, err := runBin(t, legacy, "-version")
	if err != nil {
		t.Fatalf("bob -version: %v\n%s", err, legacyErr)
	}

	for _, want := range []string{"iam-bob-eino", "intent-bob-eino", "intent-agent-model/bob", "eino-go", "cloudwego/eino"} {
		if !strings.Contains(canonOut, want) {
			t.Errorf("canonical -version missing %q:\n%s", want, canonOut)
		}
	}

	// stdout is machine-facing and identical across entry points (modulo the
	// per-process instance id).
	if stripInstance(canonOut) != stripInstance(legacyOut) {
		t.Errorf("stdout differs between entry points:\ncanonical:\n%s\nlegacy:\n%s", canonOut, legacyOut)
	}

	// The canonical binary never warns.
	if strings.Contains(canonErr, "deprecated") {
		t.Errorf("bob-eino must not print a deprecation warning: %q", canonErr)
	}
	// The legacy alias warns exactly once, on stderr only.
	if got := strings.Count(legacyErr, "deprecated"); got != 1 {
		t.Errorf("bob deprecation warning count = %d, want exactly 1:\n%s", got, legacyErr)
	}
	if !strings.Contains(legacyErr, "bob-eino") {
		t.Errorf("deprecation warning must point at bob-eino: %q", legacyErr)
	}
	if strings.Contains(legacyOut, "deprecated") {
		t.Errorf("deprecation warning leaked into stdout (breaks machine output): %q", legacyOut)
	}
}

// TestUsageErrorSurfacesOnBoth proves the no-task error path is shared too.
func TestUsageErrorSurfacesOnBoth(t *testing.T) {
	canonical, legacy := buildBoth(t)
	for name, bin := range map[string]string{"canonical": canonical, "legacy": legacy} {
		stdout, stderr, err := runBin(t, bin)
		if err == nil {
			t.Errorf("%s with no task should exit non-zero", name)
		}
		if !strings.Contains(stderr, "usage") {
			t.Errorf("%s stderr = %q, want usage", name, stderr)
		}
		// The usage now advertises the subcommand surface.
		for _, cmdWord := range []string{"plan", "run", "verify", "evidence", "doctor", "version"} {
			if !strings.Contains(stderr, cmdWord) {
				t.Errorf("%s usage missing subcommand %q:\n%s", name, cmdWord, stderr)
			}
		}
		if stdout != "" {
			t.Errorf("%s stdout must stay clean on error, got %q", name, stdout)
		}
	}
}
