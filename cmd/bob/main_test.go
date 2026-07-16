package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLISmoke is the L6 CLI smoke test: build the bob binary and exercise its
// top-level surfaces (version, usage) to prove the wiring compiles and runs.
func TestCLISmoke(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "bob")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	// -version prints the agent + engine identity and exits 0.
	out, err := exec.Command(bin, "-version").CombinedOutput()
	if err != nil {
		t.Fatalf("bob -version failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "iam-bob-eino") || !strings.Contains(string(out), "eino") {
		t.Fatalf("version output = %q, want agent + engine identity", out)
	}

	// No task prints usage and exits non-zero.
	cmd := exec.Command(bin)
	usage, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("bob with no task should exit non-zero")
	}
	if !strings.Contains(string(usage), "usage") {
		t.Fatalf("no-task output = %q, want usage", usage)
	}

	_ = os.Remove(bin)
}
