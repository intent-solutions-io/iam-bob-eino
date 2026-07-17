// Package scripts holds test-only coverage for the release/installer shell
// tooling: the full install → upgrade → rollback → uninstall lifecycle in a
// disposable HOME, checksum enforcement, version injection, and the
// never-touch-an-unrelated-bob rule.
package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const versionPkg = "github.com/intent-solutions-io/iam-bob-eino/internal/version"

// buildRelease compiles both binaries with release-style ldflags and packs a
// release-shaped archive plus its checksums file. Returns the archive and
// checksums paths.
func buildRelease(t *testing.T, version string) (archive, checksums string) {
	t.Helper()
	work := t.TempDir()
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	ldflags := "-X " + versionPkg + ".AgentVersion=" + version +
		" -X " + versionPkg + ".BuildCommit=testcommit -X " + versionPkg + ".BuildDate=2026-07-16T00:00:00Z"
	for bin, pkg := range map[string]string{"bob-eino": "./cmd/bob-eino", "bob": "./cmd/bob"} {
		cmd := exec.Command("go", "build", "-ldflags", ldflags, "-o", filepath.Join(work, bin), pkg)
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", bin, err, out)
		}
	}
	for _, extra := range []string{"LICENSE", "NOTICE", "README.md"} {
		data, err := os.ReadFile(filepath.Join(repoRoot, extra))
		if err != nil {
			t.Fatalf("read %s: %v", extra, err)
		}
		if err := os.WriteFile(filepath.Join(work, extra), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	archive = filepath.Join(t.TempDir(), "intent-bob-eino_"+version+"_test_amd64.tar.gz")
	tar := exec.Command("tar", "-czf", archive, "-C", work, ".")
	if out, err := tar.CombinedOutput(); err != nil {
		t.Fatalf("tar: %v\n%s", err, out)
	}
	sum := exec.Command("sha256sum", filepath.Base(archive))
	sum.Dir = filepath.Dir(archive)
	out, err := sum.Output()
	if err != nil {
		t.Fatal(err)
	}
	checksums = filepath.Join(filepath.Dir(archive), "checksums.txt")
	if err := os.WriteFile(checksums, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return archive, checksums
}

// runInstaller invokes install.sh with a disposable HOME and returns
// combined output.
func runInstaller(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func binDir(home string) string { return filepath.Join(home, ".local", "bin") }

func mustVersion(t *testing.T, bin string) string {
	t.Helper()
	out, err := exec.Command(bin, "version").Output()
	if err != nil {
		t.Fatalf("%s version: %v", bin, err)
	}
	return string(out)
}

func TestReleaseVersionInjectionAndFullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries; skipped in -short")
	}
	home := t.TempDir()
	v1Archive, v1Sums := buildRelease(t, "9.9.1-test")

	// --- install
	out, err := runInstaller(t, home, "install", "--archive", v1Archive, "--checksums", v1Sums)
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	canonical := filepath.Join(binDir(home), "bob-eino")

	// Version injection: the installed binary reports the injected release
	// version, commit, and date — and identifies the component.
	ver := mustVersion(t, canonical)
	for _, want := range []string{"9.9.1-test", "testcommit", "intent-bob-eino"} {
		if !strings.Contains(ver, want) {
			t.Errorf("installed version output missing %q:\n%s", want, ver)
		}
	}
	// Legacy alias installed too (ours).
	legacy := filepath.Join(binDir(home), "bob")
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy alias not installed: %v", err)
	}

	// Simulated user state that every later step must preserve.
	stateDir := filepath.Join(home, ".local", "state", "intent-solutions", "agents", "bob", "eino-go")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	evidencePath := filepath.Join(stateDir, "evidence.jsonl")
	if err := os.WriteFile(evidencePath, []byte("{\"action_id\":\"keep-me\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// --- upgrade preserves the previous binary
	v2Archive, v2Sums := buildRelease(t, "9.9.2-test")
	out, err = runInstaller(t, home, "upgrade", "--archive", v2Archive, "--checksums", v2Sums)
	if err != nil {
		t.Fatalf("upgrade: %v\n%s", err, out)
	}
	if !strings.Contains(mustVersion(t, canonical), "9.9.2-test") {
		t.Fatal("upgrade did not switch the binary")
	}
	prev := filepath.Join(binDir(home), "bob-eino.prev")
	if _, err := os.Stat(prev); err != nil {
		t.Fatalf("upgrade did not preserve the previous binary: %v", err)
	}

	// --- rollback restores the previous version
	out, err = runInstaller(t, home, "rollback")
	if err != nil {
		t.Fatalf("rollback: %v\n%s", err, out)
	}
	if !strings.Contains(mustVersion(t, canonical), "9.9.1-test") {
		t.Fatal("rollback did not restore the previous version")
	}

	// --- uninstall removes binaries only
	out, err = runInstaller(t, home, "uninstall")
	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	for _, gone := range []string{canonical, legacy, prev} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("uninstall left %s behind (or errored: %v)", gone, err)
		}
	}
	if data, err := os.ReadFile(evidencePath); err != nil || !strings.Contains(string(data), "keep-me") {
		t.Fatalf("uninstall touched user evidence: %v", err)
	}
}

func TestChecksumMismatchRefusesInstall(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries; skipped in -short")
	}
	home := t.TempDir()
	archive, _ := buildRelease(t, "9.9.3-test")
	badSums := filepath.Join(t.TempDir(), "checksums.txt")
	if err := os.WriteFile(badSums,
		[]byte(strings.Repeat("0", 64)+"  "+filepath.Base(archive)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runInstaller(t, home, "install", "--archive", archive, "--checksums", badSums)
	if err == nil {
		t.Fatalf("corrupted checksum must refuse to install:\n%s", out)
	}
	if !strings.Contains(out, "CHECKSUM MISMATCH") {
		t.Errorf("expected an explicit mismatch refusal:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(binDir(home), "bob-eino")); !os.IsNotExist(statErr) {
		t.Error("binary installed despite checksum mismatch")
	}
}

func TestMissingChecksumEntryRefusesInstall(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries; skipped in -short")
	}
	home := t.TempDir()
	archive, _ := buildRelease(t, "9.9.4-test")
	empty := filepath.Join(t.TempDir(), "checksums.txt")
	if err := os.WriteFile(empty, []byte("deadbeef  some-other-file.tar.gz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runInstaller(t, home, "install", "--archive", archive, "--checksums", empty); err == nil {
		t.Fatalf("archive without a checksum entry must be refused:\n%s", out)
	}
}

// TestUnrelatedBobBinaryIsNeverTouched: a foreign `bob` (e.g.
// iam-bob-intendant's) must survive install AND uninstall.
func TestUnrelatedBobBinaryIsNeverTouched(t *testing.T) {
	if testing.Short() {
		t.Skip("builds binaries; skipped in -short")
	}
	home := t.TempDir()
	if err := os.MkdirAll(binDir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(binDir(home), "bob")
	const foreignBody = "#!/bin/sh\necho i-am-a-different-bob\n"
	if err := os.WriteFile(foreign, []byte(foreignBody), 0o755); err != nil {
		t.Fatal(err)
	}

	archive, sums := buildRelease(t, "9.9.5-test")
	out, err := runInstaller(t, home, "install", "--archive", archive, "--checksums", sums)
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if data, _ := os.ReadFile(foreign); string(data) != foreignBody {
		t.Fatal("install overwrote an unrelated bob executable")
	}
	if !strings.Contains(out, "SKIPPING legacy alias") {
		t.Errorf("installer should report the skip:\n%s", out)
	}

	out, err = runInstaller(t, home, "uninstall")
	if err != nil {
		t.Fatalf("uninstall: %v\n%s", err, out)
	}
	if data, _ := os.ReadFile(foreign); string(data) != foreignBody {
		t.Fatal("uninstall removed an unrelated bob executable")
	}
}
