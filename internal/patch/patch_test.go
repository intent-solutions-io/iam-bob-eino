package patch

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/intent-solutions-io/iam-bob-eino/internal/workspace"
)

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// fixture creates a workspace with the given files and returns it plus the root.
func fixture(t *testing.T, files map[string]string) (*workspace.Workspace, string) {
	t.Helper()
	root := t.TempDir()
	for p, content := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ws, err := workspace.New(root)
	if err != nil {
		t.Fatal(err)
	}
	return ws, root
}

func onePatch(path, content, find, replace string) Patch {
	return Patch{
		SchemaVersion: SchemaVersion,
		Files: []FileChange{{
			Path: path, PreSHA256: hashOf(content),
			Hunks: []Hunk{{Find: find, Replace: replace, ExpectCount: strings.Count(content, find)}},
		}},
	}
}

func TestApplyReplacesAndVerifies(t *testing.T) {
	ws, root := fixture(t, map[string]string{"a.txt": "one two one\n"})
	p := onePatch("a.txt", "one two one\n", "two", "TWO")
	res, err := Apply(ws, p)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "one TWO one\n" {
		t.Errorf("content = %q", got)
	}
	fr := res.Files[0]
	if fr.PostSHA256 != hashOf("one TWO one\n") || fr.HunksApplied != 1 {
		t.Errorf("result = %+v", fr)
	}
	// Result must be evidence-safe: no content fields exist by construction,
	// spot-check the rendered form anyway.
	if strings.Contains(fmt.Sprintf("%+v", res), "TWO one") {
		t.Error("result leaked file content")
	}
}

func TestOccurrenceSelectsNth(t *testing.T) {
	const content = "x y x y x\n"
	ws, root := fixture(t, map[string]string{"a.txt": content})
	p := Patch{SchemaVersion: SchemaVersion, Files: []FileChange{{
		Path: "a.txt", PreSHA256: hashOf(content),
		Hunks: []Hunk{{Find: "x", Replace: "Z", ExpectCount: 3, Occurrence: 2}},
	}}}
	if _, err := Apply(ws, p); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "x y Z y x\n" {
		t.Errorf("content = %q, want only the 2nd occurrence replaced", got)
	}
}

func TestOccurrenceCountMismatchIsZeroWrites(t *testing.T) {
	const content = "x y x\n"
	ws, root := fixture(t, map[string]string{"a.txt": content})
	p := Patch{SchemaVersion: SchemaVersion, Files: []FileChange{{
		Path: "a.txt", PreSHA256: hashOf(content),
		Hunks: []Hunk{{Find: "x", Replace: "Z", ExpectCount: 3}}, // actually 2
	}}}
	if _, err := Apply(ws, p); !errors.Is(err, ErrOccurrenceMismatch) {
		t.Fatalf("err = %v, want ErrOccurrenceMismatch", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != content {
		t.Error("file mutated despite occurrence mismatch")
	}
}

func TestPreHashMismatchIsZeroWrites(t *testing.T) {
	ws, root := fixture(t, map[string]string{"a.txt": "current\n"})
	p := Patch{SchemaVersion: SchemaVersion, Files: []FileChange{{
		Path: "a.txt", PreSHA256: hashOf("stale content the patch was built against\n"),
		Hunks: []Hunk{{Find: "current", Replace: "next", ExpectCount: 1}},
	}}}
	if _, err := Apply(ws, p); !errors.Is(err, ErrPreHashMismatch) {
		t.Fatalf("err = %v, want ErrPreHashMismatch", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "current\n" {
		t.Error("file mutated despite pre-hash mismatch")
	}
}

// TestMultiFileFailureLeavesZeroWrites: if the SECOND file fails phase-1
// verification, the first must not have been written either.
func TestMultiFileFailureLeavesZeroWrites(t *testing.T) {
	ws, root := fixture(t, map[string]string{"a.txt": "aaa\n", "b.txt": "bbb\n"})
	p := Patch{SchemaVersion: SchemaVersion, Files: []FileChange{
		{Path: "a.txt", PreSHA256: hashOf("aaa\n"), Hunks: []Hunk{{Find: "aaa", Replace: "AAA", ExpectCount: 1}}},
		{Path: "b.txt", PreSHA256: hashOf("wrong\n"), Hunks: []Hunk{{Find: "bbb", Replace: "BBB", ExpectCount: 1}}},
	}}
	if _, err := Apply(ws, p); !errors.Is(err, ErrPreHashMismatch) {
		t.Fatalf("err = %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != "aaa\n" {
		t.Error("phase-1 failure on file 2 must leave file 1 untouched (two-phase broken)")
	}
}

func TestBinaryContentRefused(t *testing.T) {
	content := "text\x00binary"
	ws, _ := fixture(t, map[string]string{"a.bin.txt": content})
	p := onePatch("a.bin.txt", content, "text", "T")
	if _, err := Apply(ws, p); !errors.Is(err, ErrBinary) {
		t.Errorf("err = %v, want ErrBinary", err)
	}
}

func TestOversizedResultRefused(t *testing.T) {
	content := "seed\n"
	ws, root := fixture(t, map[string]string{"a.txt": content})
	p := Patch{SchemaVersion: SchemaVersion, Files: []FileChange{{
		Path: "a.txt", PreSHA256: hashOf(content),
		Hunks: []Hunk{{Find: "seed", Replace: strings.Repeat("x", MaxResultBytes+1), ExpectCount: 1}},
	}}}
	if _, err := Apply(ws, p); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "a.txt"))
	if string(got) != content {
		t.Error("oversized result must write nothing")
	}
}

func TestValidateBounds(t *testing.T) {
	valid := func() Patch { return onePatch("a.txt", "x", "x", "y") }

	p := valid()
	p.SchemaVersion = "intent-bob-eino-patch/v2"
	if err := Validate(p); !errors.Is(err, ErrMalformed) {
		t.Errorf("wrong schema: %v", err)
	}

	p = valid()
	p.Files = nil
	if err := Validate(p); !errors.Is(err, ErrMalformed) {
		t.Errorf("no files: %v", err)
	}

	p = valid()
	for i := 0; i <= MaxFiles; i++ {
		fc := p.Files[0]
		fc.Path = fmt.Sprintf("f%d.txt", i)
		p.Files = append(p.Files, fc)
	}
	if err := Validate(p); !errors.Is(err, ErrTooManyFiles) {
		t.Errorf("too many files: %v", err)
	}

	p = valid()
	for i := 0; i <= MaxHunksPerFile; i++ {
		p.Files[0].Hunks = append(p.Files[0].Hunks, Hunk{Find: "x", Replace: "y", ExpectCount: 1})
	}
	if err := Validate(p); !errors.Is(err, ErrTooManyHunks) {
		t.Errorf("too many hunks: %v", err)
	}

	p = valid()
	p.Files = append(p.Files, p.Files[0])
	if err := Validate(p); !errors.Is(err, ErrDuplicatePath) {
		t.Errorf("duplicate path: %v", err)
	}

	p = valid()
	p.Files[0].Hunks[0].ExpectCount = 0
	if err := Validate(p); !errors.Is(err, ErrMalformed) {
		t.Errorf("zero expect_count: %v", err)
	}

	p = valid()
	p.Files[0].Hunks[0].Occurrence = 5 // > expect_count 1
	if err := Validate(p); !errors.Is(err, ErrMalformed) {
		t.Errorf("occurrence beyond expect_count: %v", err)
	}
}

func TestForbiddenPaths(t *testing.T) {
	for _, bad := range []string{
		"../escape.txt", "/etc/passwd", ".git/config", "sub/.git/hooks/pre-commit",
		".env", ".env.local", "server.pem", "signing.key", "~/x.txt",
	} {
		p := onePatch(bad, "x", "x", "y")
		if err := Validate(p); !errors.Is(err, ErrForbiddenPath) {
			t.Errorf("path %q: err = %v, want ErrForbiddenPath", bad, err)
		}
	}
}

func TestParseBounds(t *testing.T) {
	if _, err := Parse([]byte(strings.Repeat("x", MaxPatchBytes+1))); !errors.Is(err, ErrTooLarge) {
		t.Errorf("oversized document: %v", err)
	}
	if _, err := Parse([]byte(`{"schema_version":"intent-bob-eino-patch/v1","files":[],"extra":1}`)); !errors.Is(err, ErrMalformed) {
		t.Errorf("unknown field: %v", err)
	}
	if _, err := Parse([]byte(`not json`)); !errors.Is(err, ErrMalformed) {
		t.Errorf("non-json: %v", err)
	}
}

func TestReplaceNth(t *testing.T) {
	cases := []struct {
		s, find, rep string
		n            int
		want         string
	}{
		{"a b a b a", "a", "X", 1, "X b a b a"},
		{"a b a b a", "a", "X", 2, "a b X b a"},
		{"a b a b a", "a", "X", 3, "a b a b X"},
		{"aaa", "aa", "X", 1, "Xa"},
	}
	for _, c := range cases {
		if got := replaceNth(c.s, c.find, c.rep, c.n); got != c.want {
			t.Errorf("replaceNth(%q,%q,%q,%d) = %q, want %q", c.s, c.find, c.rep, c.n, got, c.want)
		}
	}
}

func TestMissingFileIsError(t *testing.T) {
	ws, _ := fixture(t, map[string]string{})
	p := onePatch("nope.txt", "x", "x", "y")
	if _, err := Apply(ws, p); err == nil {
		t.Fatal("patching a missing file must error (new files are write_file's job)")
	}
}
