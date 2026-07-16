// Package cli is the single implementation behind both command entry points:
// cmd/bob-eino (canonical) and cmd/bob (legacy compatibility alias). Keeping
// the whole surface here guarantees the two binaries cannot drift — the alias
// is one deprecation line plus a call into the same Run.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/intent-solutions-io/iam-bob-eino/internal/agent"
	"github.com/intent-solutions-io/iam-bob-eino/internal/approval"
	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/governor"
	"github.com/intent-solutions-io/iam-bob-eino/internal/identity"
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
	"github.com/intent-solutions-io/iam-bob-eino/internal/provider"
	"github.com/intent-solutions-io/iam-bob-eino/internal/tools"
	"github.com/intent-solutions-io/iam-bob-eino/internal/version"
	"github.com/intent-solutions-io/iam-bob-eino/internal/workspace"
)

// Run executes one CLI invocation with the given argument list (excluding the
// program name) and returns the process exit code. Both entry points call it;
// nothing else may implement CLI behavior.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if err := run(args, stdin, stdout, stderr); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("bob-eino", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		wsPath     = fs.String("workspace", ".", "workspace root directory Bob may operate in")
		modelSel   = fs.String("model", "", "model selector provider/model (default from $INTENT_BOB_EINO_MODEL, legacy $BOB_MODEL, or deepseek/deepseek-chat)")
		allowWrite = fs.Bool("allow-writes", false, "enable file writes (still requires approval)")
		autoYes    = fs.Bool("yes", false, "auto-approve actions that require approval (non-interactive)")
		evPath     = fs.String("evidence", "", "evidence log path (default <state-dir>/evidence.jsonl outside the workspace)")
		showVer    = fs.Bool("version", false, "print version and exit")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *showVer {
		id, err := identity.New(identity.RoleCoding, "local", version.AgentVersion)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, id.Display())
		fmt.Fprintf(stdout, "  engine:    %s %s\n", version.Engine, version.EngineVersion)
		return nil
	}

	task := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if task == "" {
		return fmt.Errorf("usage: bob-eino [flags] <task>\n(use -h for flags)")
	}

	ctx := context.Background()

	ws, err := workspace.New(*wsPath)
	if err != nil {
		return err
	}

	// Evidence sink lives OUTSIDE the workspace so the agent being audited
	// cannot read, rewrite, or delete its own audit trail through its tools.
	evidencePath := *evPath
	if evidencePath == "" {
		evidencePath, err = ResolveEvidencePath(stderr)
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(evidencePath), 0o755); err != nil {
		return fmt.Errorf("create evidence dir: %w", err)
	}
	sink, err := evidence.NewJSONLSink(evidencePath)
	if err != nil {
		return err
	}
	defer sink.Close()

	pol := policy.Default()
	pol.AllowWrites = *allowWrite

	var approver approval.Approver
	if *autoYes {
		approver = approval.AutoApprove{}
	} else {
		approver = approval.Prompt{In: stdin, Out: stderr}
	}

	gov := governor.New(ws, pol, approver, sink)

	toolset, err := tools.All(gov)
	if err != nil {
		return err
	}

	cfg, err := provider.Resolve(*modelSel)
	if err != nil {
		return err
	}
	model, err := provider.New(ctx, cfg)
	if err != nil {
		return err
	}

	ag, err := agent.New(ctx, agent.Config{Model: model, Tools: toolset})
	if err != nil {
		return err
	}

	fmt.Fprintf(stderr, "%s: workspace=%s model=%s/%s writes=%v evidence=%s\n",
		version.Component, ws.Root(), cfg.Provider, cfg.Model, pol.AllowWrites, evidencePath)

	answer, err := agent.Run(ctx, ag, task, stderr)
	if err != nil {
		return err
	}

	fmt.Fprintln(stdout, strings.TrimSpace(answer))
	return nil
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
