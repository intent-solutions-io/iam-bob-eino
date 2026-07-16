package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadWriteWithinRoot(t *testing.T) {
	ws, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer ws.Close()
	if err := ws.WriteFile("sub/file.txt", []byte("hi"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	data, truncated, err := ws.ReadFileLimited("sub/file.txt", 1024)
	if err != nil || string(data) != "hi" || truncated {
		t.Fatalf("ReadFileLimited = %q trunc=%v err=%v", data, truncated, err)
	}
}

func TestRejectsTraversalAndAbsolute(t *testing.T) {
	ws, _ := New(t.TempDir())
	defer ws.Close()
	for _, rel := range []string{"../secret", "../../etc/passwd", "sub/../../outside", "/etc/passwd"} {
		if _, _, err := ws.ReadFileLimited(rel, 1024); err == nil {
			t.Errorf("ReadFileLimited(%q) = nil error, want rejection", rel)
		}
	}
}

// TestSymlinkEscapeBlocked is the H1 regression test: a symlink INSIDE the
// workspace that points outside it must not let Bob read or write beyond root.
func TestSymlinkEscapeBlocked(t *testing.T) {
	outsideDir := t.TempDir()
	secret := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(secret, []byte("TOP SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	// link -> outsideDir (a symlink inside the workspace pointing out).
	if err := os.Symlink(outsideDir, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	ws, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	// Read through the escaping symlink must fail.
	if data, _, err := ws.ReadFileLimited("link/secret.txt", 1024); err == nil {
		t.Fatalf("read through escaping symlink succeeded, got %q — H1 regression", data)
	}
	// Write through the escaping symlink must fail and must NOT create a file outside.
	if err := ws.WriteFile("link/pwned.txt", []byte("x"), 0o644); err == nil {
		t.Fatal("write through escaping symlink succeeded — H1 regression")
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "pwned.txt")); !os.IsNotExist(err) {
		t.Fatal("write escaped the workspace — H1 regression")
	}
}

func TestReadFileLimitedTruncates(t *testing.T) {
	ws, _ := New(t.TempDir())
	defer ws.Close()
	_ = ws.WriteFile("big.txt", []byte("0123456789"), 0o644)
	data, truncated, err := ws.ReadFileLimited("big.txt", 4)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || string(data) != "0123" {
		t.Fatalf("got %q trunc=%v, want 0123 truncated", data, truncated)
	}
}

func TestNewRejectsNonDir(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("New(missing) = nil error, want error")
	}
}
