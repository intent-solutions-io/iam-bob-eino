package gitstate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initRepo creates a real git repository with one commit and returns its path.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid",
			"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "a.txt")
	run("commit", "-q", "-m", "initial")
	return dir
}

func TestHeadOnCleanRepo(t *testing.T) {
	dir := initRepo(t)
	st, err := Head(dir)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if len(st.HeadSHA) != 40 {
		t.Errorf("HeadSHA = %q, want a full 40-char SHA", st.HeadSHA)
	}
	if st.Branch != "main" {
		t.Errorf("Branch = %q, want main", st.Branch)
	}
	if st.Dirty {
		t.Error("clean repo reported dirty")
	}
}

func TestHeadReportsDirtyAndChangedFiles(t *testing.T) {
	dir := initRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Head(dir)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if !st.Dirty {
		t.Error("modified repo reported clean")
	}
	changed, err := ChangedFiles(dir)
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}
	want := []string{"a.txt", "new.txt"}
	if len(changed) != len(want) {
		t.Fatalf("changed = %v, want %v", changed, want)
	}
	for i := range want {
		if changed[i] != want[i] {
			t.Errorf("changed[%d] = %q, want %q (sorted)", i, changed[i], want[i])
		}
	}
}

func TestHeadOnEmptyRepoHasNoSHA(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("git", "-C", dir, "init", "-q", "-b", "main")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	st, err := Head(dir)
	if err != nil {
		t.Fatalf("Head on unborn branch: %v", err)
	}
	if st.HeadSHA != "" {
		t.Errorf("HeadSHA = %q, want empty on a repo with no commits", st.HeadSHA)
	}
	if st.Branch != "main" {
		t.Errorf("Branch = %q, want main", st.Branch)
	}
}

func TestNotARepositoryIsTyped(t *testing.T) {
	dir := t.TempDir()
	if _, err := Head(dir); !errors.Is(err, ErrNotARepository) {
		t.Errorf("Head err = %v, want ErrNotARepository", err)
	}
	if _, err := ChangedFiles(dir); !errors.Is(err, ErrNotARepository) {
		t.Errorf("ChangedFiles err = %v, want ErrNotARepository", err)
	}
}

func TestGitUnavailableIsTyped(t *testing.T) {
	orig := lookPath
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	defer func() { lookPath = orig }()
	if _, err := Head(t.TempDir()); !errors.Is(err, ErrGitUnavailable) {
		t.Errorf("Head err = %v, want ErrGitUnavailable", err)
	}
}

func TestChangedFilesHandlesRename(t *testing.T) {
	dir := initRepo(t)
	cmd := exec.Command("git", "-C", dir, "mv", "a.txt", "b.txt")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git mv: %v\n%s", err, out)
	}
	changed, err := ChangedFiles(dir)
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}
	got := map[string]bool{}
	for _, p := range changed {
		got[p] = true
	}
	if !got["a.txt"] || !got["b.txt"] {
		t.Errorf("rename must report both sides, got %v", changed)
	}
}
