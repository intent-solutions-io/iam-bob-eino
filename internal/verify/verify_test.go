package verify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileContentMatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	content := []byte("hello world")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	v := FileContent(path, HashBytes(content))
	if !v.Verified {
		t.Fatalf("FileContent = %+v, want verified", v)
	}
}

func TestFileContentMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(path, []byte("actual"), 0o644)
	v := FileContent(path, HashBytes([]byte("expected something else")))
	if v.Verified {
		t.Fatal("FileContent verified a mismatched hash")
	}
}

func TestFileContentUnreadable(t *testing.T) {
	v := FileContent(filepath.Join(t.TempDir(), "does-not-exist"), "deadbeef")
	if v.Verified {
		t.Fatal("FileContent verified an unreadable file")
	}
}

func TestCommandExit(t *testing.T) {
	if v := CommandExit(0, 0); !v.Verified {
		t.Errorf("CommandExit(0,0) = %+v, want verified", v)
	}
	if v := CommandExit(1, 0); v.Verified {
		t.Errorf("CommandExit(1,0) = %+v, want mismatch", v)
	}
	// Label reflects the verdict for the evidence record.
	if CommandExit(1, 0).Label() != StatusMismatch {
		t.Error("failing command should label as mismatch")
	}
}

func TestHashBytes(t *testing.T) {
	if HashBytes([]byte("a")) == HashBytes([]byte("b")) {
		t.Error("distinct inputs must hash differently")
	}
	if HashBytes([]byte("a")) != HashBytes([]byte("a")) {
		t.Error("HashBytes must be deterministic")
	}
}
