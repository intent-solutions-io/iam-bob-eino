// Shared flag surface for the lifecycle subcommands (doctor/plan/run/verify/
// evidence). Only flags the user EXPLICITLY set (fs.Visit) become
// config.Overrides, so the config precedence chain (CLI > env > file >
// defaults) stays honest — an untouched flag never masks an env or file value.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/intent-solutions-io/iam-bob-eino/internal/approval"
	"github.com/intent-solutions-io/iam-bob-eino/internal/config"
	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/provider"
)

// commonFlags holds the flag values shared by the agent-running subcommands.
type commonFlags struct {
	fs          *flag.FlagSet
	configPath  string
	workspace   string
	modelSel    string
	maxSteps    int
	timeout     time.Duration
	allowWrites bool
	allowExec   bool
	yes         bool
	evidenceDir string
	jsonOut     bool
}

// newCommonFlags builds the FlagSet for a subcommand and registers the shared
// flags on it. Callers may register command-specific flags afterwards, then
// Parse, then buildConfig.
func newCommonFlags(name string, stderr io.Writer) *commonFlags {
	fs := flag.NewFlagSet("bob-eino "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	c := &commonFlags{fs: fs}
	fs.StringVar(&c.configPath, "config", "", "path to config.json (must exist when set)")
	fs.StringVar(&c.workspace, "workspace", "", "workspace root directory Bob may operate in")
	fs.StringVar(&c.modelSel, "model", "", "model selector in provider/model form (e.g. "+provider.DefaultModel+")")
	fs.IntVar(&c.maxSteps, "max-steps", 0, "maximum agent loop steps")
	fs.DurationVar(&c.timeout, "timeout", 0, "overall run timeout (0 = none)")
	fs.BoolVar(&c.allowWrites, "allow-writes", false, "enable file writes (still approval-gated)")
	fs.BoolVar(&c.allowExec, "allow-exec", false, "enable command execution (still approval-gated; requires -allow-writes)")
	fs.BoolVar(&c.yes, "yes", false, "auto-approve in-plan actions (grants NO capability; refuses plan variance)")
	fs.StringVar(&c.evidenceDir, "evidence-dir", "", "evidence directory (must resolve outside the workspace)")
	fs.BoolVar(&c.jsonOut, "json", false, "emit machine-readable JSON output")
	return c
}

// buildConfig converts only the explicitly-set flags into config.Overrides
// and runs the full merge/validation. The -model selector is split into
// separate Provider and Model overrides; both halves are required so a
// selector can never half-override the pair.
func (c *commonFlags) buildConfig(stderr io.Writer) (config.Config, error) {
	var o config.Overrides
	var selErr error
	c.fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "workspace":
			o.Workspace = &c.workspace
		case "model":
			prov, mod, ok := strings.Cut(c.modelSel, "/")
			if !ok || prov == "" || mod == "" {
				selErr = fmt.Errorf("-model %q must be in provider/model form", c.modelSel)
				return
			}
			o.Provider, o.Model = &prov, &mod
		case "max-steps":
			o.MaxSteps = &c.maxSteps
		case "timeout":
			o.Timeout = &c.timeout
		case "allow-writes":
			o.AllowWrites = &c.allowWrites
		case "allow-exec":
			o.AllowExec = &c.allowExec
		case "evidence-dir":
			o.EvidenceDir = &c.evidenceDir
		}
	})
	if selErr != nil {
		return config.Config{}, selErr
	}
	return config.Load(config.Options{ConfigPath: c.configPath, CLI: o, Warn: stderr})
}

// approver maps the approval configuration to an Approver. -yes (or approval
// mode "auto") selects AutoApprove — which approves in-plan actions only and
// structurally refuses plan-variance requests; it never grants a capability.
func (c *commonFlags) approver(cfg config.Config, stdin io.Reader, stderr io.Writer) approval.Approver {
	if c.yes || cfg.ApprovalMode == "auto" {
		return approval.AutoApprove{}
	}
	return approval.Prompt{In: stdin, Out: stderr}
}

// runContext derives the run context: a cancel-cause context (so limit
// trackers and the plan guard can cancel with a typed cause) layered under
// the configured timeout.
func runContext(cfg config.Config) (ctx context.Context, cancelCause context.CancelCauseFunc, stop func()) {
	base, cause := context.WithCancelCause(context.Background())
	if cfg.Timeout > 0 {
		tctx, cancel := context.WithTimeout(base, cfg.Timeout)
		return tctx, cause, func() { cancel(); cause(nil) }
	}
	return base, cause, func() { cause(nil) }
}

// openEvidenceSink resolves the evidence log path for a subcommand run (the
// configured EvidenceDir, else the canonical state dir with legacy migration)
// and opens the append-only sink.
func openEvidenceSink(cfg config.Config, stderr io.Writer) (*evidence.JSONLSink, string, error) {
	var path string
	if cfg.EvidenceDir != "" {
		path = filepath.Join(cfg.EvidenceDir, "evidence.jsonl")
	} else {
		p, err := ResolveEvidencePath(stderr)
		if err != nil {
			return nil, "", err
		}
		path = p
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, "", fmt.Errorf("create evidence dir: %w", err)
	}
	sink, err := evidence.NewJSONLSink(path)
	if err != nil {
		return nil, "", err
	}
	return sink, path, nil
}

// evidenceLogPath resolves the evidence log path WITHOUT opening a sink, for
// read-only commands (verify, evidence).
func evidenceLogPath(cfg config.Config, stderr io.Writer) (string, error) {
	if cfg.EvidenceDir != "" {
		return filepath.Join(cfg.EvidenceDir, "evidence.jsonl"), nil
	}
	return ResolveEvidencePath(stderr)
}
