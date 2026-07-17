// Package gitstate reads a workspace's git state (HEAD SHA, branch, dirtiness,
// changed files) through read-only `git` invocations. It is the single git
// helper for the plan/run/verify lifecycle: plan records the starting SHA, the
// plan-variance guard re-checks it before every risky action, and the receipt
// records the end state.
//
// The package degrades cleanly: a missing git binary or a non-repository
// workspace returns a typed sentinel (ErrGitUnavailable / ErrNotARepository)
// so callers can skip git-bound checks instead of failing the whole run. It
// never mutates repository state — every subprocess is a read-only query.
package gitstate

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Typed sentinel errors. Callers match with errors.Is and degrade (skip
// git-bound checks) rather than treating either as fatal.
var (
	// ErrGitUnavailable means no `git` binary is on PATH.
	ErrGitUnavailable = errors.New("gitstate: git binary not found")
	// ErrNotARepository means the directory is not inside a git work tree.
	ErrNotARepository = errors.New("gitstate: not a git repository")
)

// State is a read-only snapshot of a repository's position.
type State struct {
	// HeadSHA is the full commit SHA of HEAD; empty in a repository with no
	// commits yet (unborn branch).
	HeadSHA string
	// Branch is the current branch name, or "HEAD" when detached.
	Branch string
	// Dirty reports whether the work tree has any staged, unstaged, or
	// untracked changes.
	Dirty bool
}

// lookPath is swappable in tests so the git-missing path is testable without
// mutating PATH for the whole process.
var lookPath = exec.LookPath

// Head returns the current git state of dir. A repository with no commits
// yields an empty HeadSHA (not an error); a missing git binary or non-repo
// directory yields the typed sentinels above.
func Head(dir string) (State, error) {
	if err := ensureRepo(dir); err != nil {
		return State{}, err
	}
	var st State
	// HEAD SHA: absent (unborn branch) is a valid state, not an error.
	if out, err := runGit(dir, "rev-parse", "--verify", "HEAD"); err == nil {
		st.HeadSHA = strings.TrimSpace(out)
	}
	// Branch: symbolic-ref works on an unborn branch too; detached HEAD has no
	// symbolic ref and is reported as "HEAD".
	if out, err := runGit(dir, "symbolic-ref", "--short", "-q", "HEAD"); err == nil && strings.TrimSpace(out) != "" {
		st.Branch = strings.TrimSpace(out)
	} else {
		st.Branch = "HEAD"
	}
	porcelain, err := runGit(dir, "status", "--porcelain")
	if err != nil {
		return State{}, fmt.Errorf("gitstate: status: %w", err)
	}
	st.Dirty = strings.TrimSpace(porcelain) != ""
	return st, nil
}

// ChangedFiles returns the sorted set of repository-relative paths with any
// staged, unstaged, or untracked change. Renames contribute both sides.
func ChangedFiles(dir string) ([]string, error) {
	if err := ensureRepo(dir); err != nil {
		return nil, err
	}
	porcelain, err := runGit(dir, "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("gitstate: status: %w", err)
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(porcelain, "\n") {
		if len(line) < 4 {
			continue
		}
		// Porcelain v1: two status columns, a space, then the path(s);
		// a rename is "R  old -> new".
		entry := line[3:]
		for _, p := range strings.SplitN(entry, " -> ", 2) {
			p = strings.Trim(strings.TrimSpace(p), `"`)
			if p != "" {
				seen[p] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// ensureRepo verifies git exists and dir is inside a work tree.
func ensureRepo(dir string) error {
	if _, err := lookPath("git"); err != nil {
		return ErrGitUnavailable
	}
	if _, err := runGit(dir, "rev-parse", "--is-inside-work-tree"); err != nil {
		return ErrNotARepository
	}
	return nil
}

// runGit executes one read-only git query in dir and returns raw stdout.
// The output is NOT trimmed here: porcelain status lines are positional (the
// two status columns may be spaces), so trimming would corrupt the first
// line. Call sites that want a single token trim themselves. The environment
// neutralizes prompts and optional locks so a query can never hang on
// credentials or contend with a concurrent git process.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
	)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
