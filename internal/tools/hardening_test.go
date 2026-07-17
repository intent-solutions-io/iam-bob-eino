package tools

import (
	"strings"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/approval"
)

// TestDangerousFlagEqualsFormRejected proves the equals form of a config/helper
// flag cannot smuggle an arbitrary-exec vector past the check (the pre-slice
// bug: exact-token matching missed "--exec-path=/evil").
func TestDangerousFlagEqualsFormRejected(t *testing.T) {
	cases := [][]string{
		{"git", "--exec-path=/evil"},
		{"git", "--config=core.pager=/evil"},
		{"git", "--git-dir=/tmp/evil"},
		{"go", "test", "-exec=/evil"},
		{"go", "build", "-toolexec=/evil"},
		{"go", "test", "-ldflags=-X"},
	}
	for _, argv := range cases {
		if err := checkDangerousFlags(argv); err == nil {
			t.Errorf("checkDangerousFlags(%v) = nil, want rejection", argv)
		}
	}
}

// TestDangerousFlagSeparateFormRejected keeps the separate-token form covered.
func TestDangerousFlagSeparateFormRejected(t *testing.T) {
	for _, argv := range [][]string{
		{"git", "-c", "core.pager=x"},
		{"go", "test", "-exec", "/evil"},
		{"go", "build", "-toolexec", "/evil"},
	} {
		if err := checkDangerousFlags(argv); err == nil {
			t.Errorf("checkDangerousFlags(%v) = nil, want rejection", argv)
		}
	}
}

// TestSafeCommandFlagsAllowed proves ordinary safe flags still pass.
func TestSafeCommandFlagsAllowed(t *testing.T) {
	for _, argv := range [][]string{
		{"go", "test", "./..."},
		{"go", "test", "-race", "./..."},
		{"go", "vet", "./..."},
		{"git", "status", "--short"},
	} {
		if err := checkDangerousFlags(argv); err != nil {
			t.Errorf("checkDangerousFlags(%v) = %v, want nil", argv, err)
		}
	}
}

// TestIsDotGitPath covers the .git detection used by write_file and apply_patch.
func TestIsDotGitPath(t *testing.T) {
	for _, p := range []string{".git", ".git/config", ".git/hooks/pre-commit", "./.git/config"} {
		if !isDotGitPath(p) {
			t.Errorf("isDotGitPath(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"main.go", "internal/git.go", "gitignore", ".gitignore"} {
		if isDotGitPath(p) {
			t.Errorf("isDotGitPath(%q) = true, want false", p)
		}
	}
}

// TestWriteFileRefusesDotGit closes the pre-slice gap where write_file could
// write into .git/** (only search skipped it).
func TestWriteFileRefusesDotGit(t *testing.T) {
	dir := t.TempDir()
	g, _ := newGov(t, dir, true, approval.AutoApprove{})
	out := invoke(t, newWriteFile, g, `{"path":".git/hooks/pre-commit","content":"#!/bin/sh\necho pwned"}`)
	if !strings.Contains(out, "DENIED") {
		t.Errorf("write into .git should be denied, got: %q", out)
	}
}
