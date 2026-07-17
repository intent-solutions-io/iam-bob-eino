// Package cli is the single implementation behind both command entry points:
// cmd/bob-eino (canonical) and cmd/bob (legacy compatibility alias). Keeping
// the whole surface here guarantees the two binaries cannot drift — the alias
// is one deprecation line plus a call into the same Run.
//
// The surface is subcommand-shaped (version | doctor | plan | run | verify |
// evidence). The original flat one-shot form ("bob-eino [flags] <task>") is
// kept as a deprecated implicit fallback; "bob-eino -- <task>" forces the
// flat form when a task happens to start with a command word.
package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
)

const usageText = `usage: bob-eino <command> [flags]

commands:
  version    print identity, build, and schema versions
  doctor     run preflight environment and configuration checks
  plan       produce a hashed, read-only plan artifact for a task
  run        execute a previously saved plan under governance
  verify     re-verify a sealed run receipt against recorded evidence
  evidence   list, show, and chain-verify the local evidence log

one-shot (deprecated): bob-eino [flags] <task>
  use 'bob-eino -- <task>' when the task starts with a command word
  use 'bob-eino <command> -h' for command flags`

// exitCodeError carries a non-zero exit code out of a command that already
// printed its own diagnostics; Run maps it to the code without adding an
// "error:" line.
type exitCodeError int

func (e exitCodeError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

// Run executes one CLI invocation with the given argument list (excluding the
// program name) and returns the process exit code. Both entry points call it;
// nothing else may implement CLI behavior.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	err := dispatch(args, stdin, stdout, stderr)
	if err == nil {
		return 0
	}
	// -h/-help on any FlagSet: usage already went to stderr; that is a
	// successful help request, not a failure.
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	var code exitCodeError
	if errors.As(err, &code) {
		return int(code)
	}
	fmt.Fprintln(stderr, "error:", err)
	return 1
}

// dispatch routes to a subcommand, or falls through to the deprecated flat
// one-shot form. "--" as the first argument forces the flat form.
func dispatch(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "version":
			return cmdVersion(args[1:], stdout, stderr)
		case "doctor":
			return cmdDoctor(args[1:], stdout, stderr)
		case "plan":
			return cmdPlan(args[1:], stdin, stdout, stderr)
		case "run":
			return cmdRun(args[1:], stdin, stdout, stderr)
		case "verify":
			return cmdVerify(args[1:], stdout, stderr)
		case "evidence":
			return cmdEvidence(args[1:], stdout, stderr)
		case "help", "-help", "--help", "-h":
			fmt.Fprintln(stdout, usageText)
			return nil
		case "--":
			return runFlat(args[1:], stdin, stdout, stderr)
		}
	}
	return runFlat(args, stdin, stdout, stderr)
}

// StateDir returns the canonical per-user state directory, OUTSIDE any
// workspace, where the evidence log lives so an audited agent cannot reach it
// through its tools. The path is namespaced by organization, persona, and
// runtime so no two agent runtimes ever share (or fight over) a state dir:
//
//	$XDG_STATE_HOME/intent-solutions/agents/bob/eino-go/
func StateDir() string {
	return filepath.Join(stateBase(), "intent-solutions", "agents", "bob", "eino-go")
}

// PlansDir is where sealed plan artifacts are saved — inside the state dir,
// outside every workspace, so a plan can never be edited by the workspace it
// governs.
func PlansDir() string { return filepath.Join(StateDir(), "plans") }

// ReceiptsDir is where sealed run receipts are saved, same containment rule.
func ReceiptsDir() string { return filepath.Join(StateDir(), "receipts") }

// LegacyStateDir returns the pre-contract state directory ("iam-bob-eino/").
// It is still discovered for reads and never destructively migrated.
func LegacyStateDir() string {
	return filepath.Join(stateBase(), "iam-bob-eino")
}

func stateBase() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "state")
	}
	return os.TempDir()
}

// ResolveEvidencePath returns the default evidence log path. New writes always
// go to the canonical state dir. If a legacy evidence log exists and the
// canonical one does not, the legacy log is hash-verified and, when intact,
// copied (never moved) to the canonical location so the chain continues in
// place; a broken legacy chain is left untouched and a fresh canonical log
// starts. The migration is idempotent: once the canonical file exists it is
// used unconditionally.
func ResolveEvidencePath(stderr io.Writer) (string, error) {
	canonical := filepath.Join(StateDir(), "evidence.jsonl")
	legacy := filepath.Join(LegacyStateDir(), "evidence.jsonl")

	if _, err := os.Stat(canonical); err == nil {
		return canonical, nil // already established — idempotent fast path
	}
	if _, err := os.Stat(legacy); err != nil {
		return canonical, nil // no legacy state; fresh canonical log
	}

	records, raw, err := readEvidenceLog(legacy)
	if err != nil {
		fmt.Fprintf(stderr, "warning: legacy evidence log unreadable (%v); starting fresh at %s (legacy kept at %s)\n", err, canonical, legacy)
		return canonical, nil
	}
	if broken := evidence.VerifyChain(records); broken != -1 {
		fmt.Fprintf(stderr, "warning: legacy evidence chain broken at record %d; NOT copying; starting fresh at %s (legacy kept at %s)\n", broken, canonical, legacy)
		return canonical, nil
	}
	if err := os.MkdirAll(filepath.Dir(canonical), 0o755); err != nil {
		return "", fmt.Errorf("create state dir: %w", err)
	}
	if err := os.WriteFile(canonical, raw, 0o644); err != nil {
		return "", fmt.Errorf("copy legacy evidence: %w", err)
	}
	fmt.Fprintf(stderr, "note: copied legacy evidence log to %s (legacy kept at %s)\n", canonical, legacy)
	return canonical, nil
}

// readEvidenceLog parses a JSONL evidence file into records, returning the raw
// bytes too so a verified copy is byte-identical to the source.
func readEvidenceLog(path string) ([]evidence.Record, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var records []evidence.Record
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec evidence.Record
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, nil, fmt.Errorf("parse evidence line: %w", err)
		}
		records = append(records, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	return records, raw, nil
}
