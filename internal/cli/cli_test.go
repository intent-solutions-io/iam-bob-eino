package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
)

// --- state paths ---

func TestStateDirIsCanonicalNamespace(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg")
	if got, want := StateDir(), filepath.Join("/xdg", "intent-solutions", "agents", "bob", "eino-go"); got != want {
		t.Errorf("StateDir = %q, want %q", got, want)
	}
	if got, want := LegacyStateDir(), filepath.Join("/xdg", "iam-bob-eino"); got != want {
		t.Errorf("LegacyStateDir = %q, want %q", got, want)
	}
}

func TestStateDirNeverEndsAtBarePersona(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg")
	// The persona segment is allowed mid-path ("agents/bob/eino-go" separates
	// persona from runtime); what is forbidden is a bare "bob" LEAF that
	// another runtime would also claim, or the pre-contract flat name.
	if strings.HasSuffix(StateDir(), string(filepath.Separator)+"bob") {
		t.Errorf("StateDir %q must not end at the bare persona", StateDir())
	}
	if strings.Contains(StateDir(), "iam-bob-eino") {
		t.Errorf("canonical StateDir %q must not reuse the legacy name", StateDir())
	}
}

// writeLegacyEvidence writes n chained records into the legacy state dir and
// returns the legacy path and raw bytes.
func writeLegacyEvidence(t *testing.T, n int) (string, []byte) {
	t.Helper()
	legacyDir := LegacyStateDir()
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(legacyDir, "evidence.jsonl")
	sink, err := evidence.NewJSONLSink(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if err := sink.Write(evidence.Record{ActionID: string(rune('a' + i))}); err != nil {
			t.Fatal(err)
		}
	}
	sink.Close()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, raw
}

func TestResolveEvidencePathFreshInstall(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var stderr bytes.Buffer
	got, err := ResolveEvidencePath(&stderr)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(StateDir(), "evidence.jsonl"); got != want {
		t.Errorf("path = %q, want canonical %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Errorf("fresh install must be silent, got %q", stderr.String())
	}
}

func TestResolveEvidencePathCopiesIntactLegacyNonDestructively(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	legacyPath, legacyRaw := writeLegacyEvidence(t, 3)

	var stderr bytes.Buffer
	got, err := ResolveEvidencePath(&stderr)
	if err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(StateDir(), "evidence.jsonl")
	if got != canonical {
		t.Errorf("path = %q, want canonical %q", got, canonical)
	}
	// Copied byte-identically; legacy left in place.
	copied, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatalf("canonical copy missing: %v", err)
	}
	if !bytes.Equal(copied, legacyRaw) {
		t.Error("canonical copy must be byte-identical to the verified legacy log")
	}
	still, err := os.ReadFile(legacyPath)
	if err != nil || !bytes.Equal(still, legacyRaw) {
		t.Error("legacy log must remain untouched (non-destructive)")
	}
	if !strings.Contains(stderr.String(), "copied legacy evidence") {
		t.Errorf("expected copy note, got %q", stderr.String())
	}

	// Idempotent: a second resolve keeps the canonical file, no re-copy chatter.
	stderr.Reset()
	got2, err := ResolveEvidencePath(&stderr)
	if err != nil {
		t.Fatal(err)
	}
	if got2 != canonical {
		t.Errorf("second resolve = %q, want %q", got2, canonical)
	}
	if stderr.Len() != 0 {
		t.Errorf("second resolve must be silent, got %q", stderr.String())
	}
}

func TestResolveEvidencePathRefusesBrokenLegacyChain(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	legacyPath, legacyRaw := writeLegacyEvidence(t, 2)
	// Tamper the log: flip a hash character inside the first record.
	tampered := strings.Replace(string(legacyRaw), `"action_id":"a"`, `"action_id":"z"`, 1)
	if tampered == string(legacyRaw) {
		t.Fatal("tamper failed to change anything")
	}
	if err := os.WriteFile(legacyPath, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	got, err := ResolveEvidencePath(&stderr)
	if err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(StateDir(), "evidence.jsonl")
	if got != canonical {
		t.Errorf("path = %q, want canonical %q", got, canonical)
	}
	if _, err := os.Stat(canonical); !os.IsNotExist(err) {
		t.Error("broken legacy chain must NOT be copied to canonical")
	}
	if !strings.Contains(stderr.String(), "chain broken") {
		t.Errorf("expected chain-broken warning, got %q", stderr.String())
	}
	// Legacy stays for forensics.
	if _, err := os.Stat(legacyPath); err != nil {
		t.Error("legacy log must be kept even when broken")
	}
}

// --- CLI surface ---

func TestRunVersionUsesStructuredIdentity(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"-version"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"Bob — Eino/Go runtime", "iam-bob-eino", "intent-bob-eino",
		"intent-agent-model/bob", "eino-go", "cloudwego/eino",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("-version output missing %q:\n%s", want, out)
		}
	}
	// The machine lines never present bare "bob" as a machine key: every
	// occurrence of "bob" is part of a longer id or the persona display line.
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		for _, w := range f {
			if w == "bob" {
				t.Errorf("bare machine token \"bob\" in -version output line %q", line)
			}
		}
	}
}

func TestRunNoTaskFailsWithUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(nil, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("no task must exit non-zero")
	}
	if !strings.Contains(stderr.String(), "usage") {
		t.Errorf("stderr = %q, want usage", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must stay clean on usage error, got %q", stdout.String())
	}
}
