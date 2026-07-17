// The deprecated flat one-shot form: "bob-eino [flags] <task>". Kept so
// existing invocations keep working; it prints a single migration note to
// stderr when a task actually runs (never for -version / -h, and never on
// stdout, so machine-read output is unaffected).
package cli

import (
	"context"
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

func runFlat(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("bob-eino", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		wsPath     = fs.String("workspace", ".", "workspace root directory Bob may operate in")
		modelSel   = fs.String("model", "", "model selector provider/model (default from $INTENT_BOB_EINO_MODEL, legacy $BOB_MODEL, or "+provider.DefaultModel+")")
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
		return fmt.Errorf("%s", usageText)
	}

	// One migration note per flat-form task run, stderr only.
	fmt.Fprintln(stderr, "note: the flat one-shot form is deprecated; use 'bob-eino plan' + 'bob-eino run' (or 'bob-eino -- <task>' to force the one-shot form)")

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

	answer, tokenUsage, err := agent.Run(ctx, ag, task, stderr)
	if err != nil {
		return err
	}
	if tokenUsage.Turns > 0 {
		fmt.Fprintf(stderr, "%s: usage total=%d tokens over %d model turns\n",
			version.Component, tokenUsage.TotalTokens, tokenUsage.Turns)
	}

	fmt.Fprintln(stdout, strings.TrimSpace(answer))
	return nil
}
