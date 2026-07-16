// Package workspace confines every file operation Bob performs to a single root
// directory. All file I/O goes through an os.Root, which refuses any path — even
// one reached through a symlink inside the workspace — that resolves outside the
// root. Callers never touch os.ReadFile/os.WriteFile directly, so a crafted
// symlink in a target repository cannot make Bob read host secrets or write
// outside the workspace.
package workspace

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Workspace is an absolute root directory that bounds all agent file access.
type Workspace struct {
	root   string
	fsRoot *os.Root
}

// New resolves root to an absolute, symlink-evaluated path, verifies it is a
// directory, and opens a symlink-safe os.Root over it.
func New(root string) (*Workspace, error) {
	if root == "" {
		return nil, fmt.Errorf("workspace root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat workspace root %q: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace root %q is not a directory", abs)
	}
	fsRoot, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("open workspace root: %w", err)
	}
	return &Workspace{root: abs, fsRoot: fsRoot}, nil
}

// Root returns the absolute workspace root.
func (w *Workspace) Root() string { return w.root }

// Check validates a workspace-relative path lexically (absolute paths and "../"
// escapes are rejected) so a tool can classify a boundary violation as a policy
// denial before attempting I/O. Symlink escapes that pass this lexical check are
// still caught by os.Root at I/O time.
func (w *Workspace) Check(rel string) error {
	_, err := clean(rel)
	return err
}

// Close releases the underlying root handle.
func (w *Workspace) Close() error { return w.fsRoot.Close() }

// FS returns a symlink-safe fs.FS rooted at the workspace, for walking.
func (w *Workspace) FS() fs.FS { return w.fsRoot.FS() }

// clean validates a workspace-relative path lexically before it reaches os.Root.
// os.Root is the real containment control; this gives a clear early error for
// absolute paths and obvious traversal, and normalizes "" / "." to ".".
func clean(rel string) (string, error) {
	if rel == "" || rel == "." {
		return ".", nil
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute paths are not allowed: %q", rel)
	}
	c := filepath.Clean(rel)
	if c == ".." || strings.HasPrefix(c, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace root: %q", rel)
	}
	return c, nil
}

// ReadFileLimited reads up to limit+1 bytes of a workspace file through the
// symlink-safe root. The extra byte lets the caller detect truncation without
// loading an arbitrarily large file into memory.
func (w *Workspace) ReadFileLimited(rel string, limit int64) (data []byte, truncated bool, err error) {
	c, err := clean(rel)
	if err != nil {
		return nil, false, err
	}
	f, err := w.fsRoot.OpenFile(c, os.O_RDONLY, 0)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(buf)) > limit {
		return buf[:limit], true, nil
	}
	return buf, false, nil
}

// Stat returns file info for a workspace path through the symlink-safe root.
func (w *Workspace) Stat(rel string) (os.FileInfo, error) {
	c, err := clean(rel)
	if err != nil {
		return nil, err
	}
	return w.fsRoot.Stat(c)
}

// ReadDir lists a workspace directory through the symlink-safe root.
func (w *Workspace) ReadDir(rel string) ([]os.DirEntry, error) {
	c, err := clean(rel)
	if err != nil {
		return nil, err
	}
	f, err := w.fsRoot.Open(c)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.ReadDir(-1)
}

// WriteFile creates or overwrites a workspace file through the symlink-safe
// root, creating parent directories as needed. A symlink that would escape the
// root causes an error rather than an escape.
func (w *Workspace) WriteFile(rel string, data []byte, perm os.FileMode) error {
	c, err := clean(rel)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(c); dir != "." {
		if err := w.fsRoot.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return w.fsRoot.WriteFile(c, data, perm)
}

// Rel returns the workspace-relative form of an absolute path for display.
func (w *Workspace) Rel(abs string) (string, error) {
	rel, err := filepath.Rel(w.root, abs)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return "", nil
	}
	return rel, nil
}
