// Command bob is the local CLI surface for the Intent Agent Model coding agent.
// It wires a workspace, the policy/approval boundary, the evidence sink, the
// governed tools, and a provider-neutral model into an Eino agent, then runs one
// task and prints Bob's answer plus an evidence summary.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/intent-solutions-io/iam-bob-eino/internal/agent"
	"github.com/intent-solutions-io/iam-bob-eino/internal/approval"
	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/governor"
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
	"github.com/intent-solutions-io/iam-bob-eino/internal/provider"
	"github.com/intent-solutions-io/iam-bob-eino/internal/tools"
	"github.com/intent-solutions-io/iam-bob-eino/internal/version"
	"github.com/intent-solutions-io/iam-bob-eino/internal/workspace"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		wsPath     = flag.String("workspace", ".", "workspace root directory Bob may operate in")
		modelSel   = flag.String("model", "", "model selector provider/model (default from $BOB_MODEL or deepseek/deepseek-chat)")
		allowWrite = flag.Bool("allow-writes", false, "enable file writes (still requires approval)")
		autoYes    = flag.Bool("yes", false, "auto-approve actions that require approval (non-interactive)")
		evPath     = flag.String("evidence", "", "evidence log path (default <workspace>/.bob/evidence.jsonl)")
		showVer    = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("%s %s (engine %s %s)\n", version.Agent, version.Bob, version.Engine, version.EngineVersion)
		return nil
	}

	task := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if task == "" {
		return fmt.Errorf("usage: bob [flags] <task>\n(use -h for flags)")
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
		evidencePath = filepath.Join(stateDir(), "evidence.jsonl")
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
		approver = approval.Prompt{In: os.Stdin, Out: os.Stderr}
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

	fmt.Fprintf(os.Stderr, "bob: workspace=%s model=%s/%s writes=%v evidence=%s\n",
		ws.Root(), cfg.Provider, cfg.Model, pol.AllowWrites, evidencePath)

	answer, err := agent.Run(ctx, ag, task, os.Stderr)
	if err != nil {
		return err
	}

	fmt.Println(strings.TrimSpace(answer))
	return nil
}

// stateDir returns a per-user state directory OUTSIDE any workspace, where the
// evidence log lives so an audited agent cannot reach it through its tools.
func stateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "iam-bob-eino")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "state", "iam-bob-eino")
	}
	return filepath.Join(os.TempDir(), "iam-bob-eino")
}
