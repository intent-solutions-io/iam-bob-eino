// The run subcommand: execute a previously saved, hash-verified plan under
// full governance — plan-variance guard, usage limits, approval boundary —
// then independently verify the outcome (internal/runverify) and seal a run
// receipt. Exit 0 only when the run completed AND the model-free verifier
// says verified (or verified_with_warnings); every abnormal end maps to a
// typed final status, never a claimed success.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"

	"github.com/intent-solutions-io/iam-bob-eino/internal/agent"
	"github.com/intent-solutions-io/iam-bob-eino/internal/config"
	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/gitstate"
	"github.com/intent-solutions-io/iam-bob-eino/internal/governor"
	"github.com/intent-solutions-io/iam-bob-eino/internal/limits"
	"github.com/intent-solutions-io/iam-bob-eino/internal/plan"
	"github.com/intent-solutions-io/iam-bob-eino/internal/planguard"
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
	"github.com/intent-solutions-io/iam-bob-eino/internal/receipt"
	"github.com/intent-solutions-io/iam-bob-eino/internal/runverify"
	"github.com/intent-solutions-io/iam-bob-eino/internal/tools"
	"github.com/intent-solutions-io/iam-bob-eino/internal/version"
	"github.com/intent-solutions-io/iam-bob-eino/internal/workspace"
)

func cmdRun(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	c := newCommonFlags("run", stderr)
	planRef := c.fs.String("plan", "", "plan id or plan file path (required)")
	if err := c.fs.Parse(args); err != nil {
		return err
	}
	if *planRef == "" {
		return fmt.Errorf("usage: bob-eino run -plan <id|path> [flags]")
	}
	cfg, err := c.buildConfig(stderr)
	if err != nil {
		return err
	}
	if cfg.Workspace == "" {
		cfg.Workspace = "."
	}

	p, err := plan.Load(resolvePlanPath(*planRef))
	if err != nil {
		return err
	}

	ws, err := workspace.New(cfg.Workspace)
	if err != nil {
		return err
	}
	defer ws.Close()

	// --- Pre-flight typed gates: fail closed BEFORE any model or tool runs.
	if ws.Root() != p.WorkspaceIdentity {
		return fmt.Errorf("run: workspace identity mismatch: plan was made for %q, current workspace is %q", p.WorkspaceIdentity, ws.Root())
	}
	if p.WorkspaceStartSHA != "" {
		st, gerr := gitstate.Head(ws.Root())
		if gerr != nil {
			return fmt.Errorf("run: plan_invalidated: plan is SHA-pinned but workspace HEAD is unreadable: %w", gerr)
		}
		if st.HeadSHA != p.WorkspaceStartSHA {
			return fmt.Errorf("run: plan_invalidated: workspace HEAD %s no longer matches the plan's start SHA %s — re-plan", st.HeadSHA, p.WorkspaceStartSHA)
		}
	}
	granted := map[string]bool{"writes": cfg.AllowWrites, "exec": cfg.AllowExec}
	grantFlag := map[string]string{"writes": "-allow-writes", "exec": "-allow-exec"}
	for _, capability := range p.RequiredCapabilities {
		if !granted[capability] {
			return fmt.Errorf("run: the plan requires the %q capability, which was not granted — pass %s", capability, grantFlag[capability])
		}
	}
	if !strings.EqualFold(cfg.Provider, p.Provider) || cfg.Model != p.Model {
		return fmt.Errorf("run: provider/model %s/%s does not match the plan's %s/%s — pass '-model %s/%s' or re-plan",
			cfg.Provider, cfg.Model, p.Provider, p.Model, p.Provider, p.Model)
	}

	// --- Governance assembly.
	sink, evPath, err := openEvidenceSink(cfg, stderr)
	if err != nil {
		return err
	}
	defer sink.Close()

	pol := policy.Default()
	pol.AllowWrites = cfg.AllowWrites
	pol.AllowExec = cfg.AllowExec

	gov := governor.New(ws, pol, c.approver(cfg, stdin, stderr), sink)
	// Bind identity and evidence correlation to this run: every record this
	// run emits carries the run id as its correlation id.
	gov.ID = gov.ID.WithRun()
	gov.Corr = gov.ID.RunID

	ctx, cancelCause, stop := runContext(cfg)
	defer stop()

	headFn := func() (string, error) {
		st, herr := gitstate.Head(ws.Root())
		return st.HeadSHA, herr
	}
	var guardHead planguard.HeadFunc
	if p.WorkspaceStartSHA != "" {
		guardHead = headFn
	}
	gov.Guard = planguard.New(p, guardHead, cancelCause)
	tracker := limits.NewTracker(limits.Default(), cancelCause)
	gov.Limits = tracker

	toolset, err := tools.All(gov)
	if err != nil {
		return err
	}
	model, err := modelFactory(ctx, cfg)
	if err != nil {
		return err
	}
	ag, err := agent.New(ctx, agent.Config{Model: model, Tools: toolset, MaxIterations: cfg.MaxSteps})
	if err != nil {
		return err
	}

	startedAt := now().UTC().Format(time.RFC3339)
	fmt.Fprintf(stderr, "%s run: plan=%s run=%s workspace=%s model=%s/%s writes=%v exec=%v evidence=%s\n",
		version.Component, p.PlanID, gov.ID.RunID, ws.Root(), cfg.Provider, cfg.Model, cfg.AllowWrites, cfg.AllowExec, evPath)

	answer, tokenUsage, runErr := agent.Run(ctx, ag, runPrompt(p), stderr)
	status := classifyRunOutcome(ctx, runErr, tracker)
	if tokenUsage.Turns > 0 {
		fmt.Fprintf(stderr, "%s run: usage prompt=%d completion=%d total=%d cached=%d turns=%d\n",
			version.Component, tokenUsage.PromptTokens, tokenUsage.CompletionTokens,
			tokenUsage.TotalTokens, tokenUsage.CachedTokens, tokenUsage.Turns)
	}

	// --- Post-run observation (never trusted claims).
	var endSHA string
	var changed []string
	if st, gerr := gitstate.Head(ws.Root()); gerr == nil {
		endSHA = st.HeadSHA
		if cf, cerr := gitstate.ChangedFiles(ws.Root()); cerr == nil {
			changed = cf
		}
	}

	runRecords := loadRunEvidence(evPath, gov.Corr, stderr)
	summary := summarizeEvidence(runRecords)
	acceptance := acceptanceResults(runRecords, p.AcceptanceChecks)

	vin := runverify.Input{
		WorkspaceRoot: ws.Root(),
		Plan: runverify.Plan{
			WorkspaceRoot: p.WorkspaceIdentity,
			ProposedFiles: p.ProposedFiles,
			StartSHA:      p.WorkspaceStartSHA,
			EndSHA:        endSHA,
		},
		Evidence:       runRecords,
		ChangedFiles:   changed,
		Acceptance:     acceptance,
		RequiredChecks: p.AcceptanceChecks,
		AgentClaim:     answer,
	}
	if endSHA != "" {
		vin.GitState = headFn
	}
	verdict := runverify.Verify(vin)

	finalStatus := status
	if finalStatus == "completed" {
		finalStatus = verdict.Result
	}

	agentID := gov.ID
	rec := receipt.RunReceipt{
		RunID:                  gov.ID.RunID,
		PlanID:                 p.PlanID,
		PlanHash:               p.ContentHash,
		Task:                   p.Task,
		AgentName:              version.Agent,
		AgentVersion:           version.AgentVersion,
		AgentIdentity:          &agentID,
		Engine:                 version.Engine,
		EngineVersion:          version.EngineVersion,
		Provider:               cfg.Provider,
		Model:                  cfg.Model,
		WorkspaceIdentity:      ws.Root(),
		WorkspaceStartSHA:      p.WorkspaceStartSHA,
		WorkspaceEndSHA:        endSHA,
		RequestedCapabilities:  p.RequiredCapabilities,
		AuthorizedCapabilities: grantedCapabilities(cfg),
		PolicyDecisions:        summary.policyDecisions,
		Approvals:              summary.approvals,
		ToolCalls:              len(runRecords),
		FilesChanged:           changed,
		PatchesApplied:         summary.patches,
		CommandsRun:            summary.commands,
		TestResults:            acceptanceStrings(acceptance),
		AgentClaim:             answer,
		ExecutionResult:        status,
		VerifierResult:         verdict.Result,
		FinalStatus:            finalStatus,
		StartedAt:              startedAt,
		CompletedAt:            now().UTC().Format(time.RFC3339),
		Usage:                  usageMap(tokenUsage),
		Authority:              receipt.AuthorityLocalUntrusted,
	}
	sealed, err := receipt.Seal(rec)
	if err != nil {
		return err
	}
	receiptPath, err := receipt.Save(sealed, ReceiptsDir())
	if err != nil {
		return err
	}

	if c.jsonOut {
		if werr := writeJSON(stdout, map[string]any{
			"run_id":       sealed.RunID,
			"plan_id":      sealed.PlanID,
			"receipt":      receiptPath,
			"final_status": sealed.FinalStatus,
			"verifier":     verdict.Result,
			"checks":       verdict.Checks,
			"failures":     verdict.Failures,
			"warnings":     verdict.Warnings,
		}); werr != nil {
			return werr
		}
	} else {
		fmt.Fprintf(stdout, "run_id: %s\nreceipt: %s\nfinal_status: %s\nverifier: %s\n",
			sealed.RunID, receiptPath, sealed.FinalStatus, verdict.Result)
		for _, f := range verdict.Failures {
			fmt.Fprintf(stdout, "failure: %s\n", f)
		}
		for _, w := range verdict.Warnings {
			fmt.Fprintf(stdout, "warning: %s\n", w)
		}
	}

	if status != "completed" {
		fmt.Fprintf(stderr, "run: stopped with status %s (receipt sealed; no retry, no auto-continue)\n", status)
		return exitCodeError(1)
	}
	if verdict.Result != runverify.ResultVerified && verdict.Result != runverify.ResultVerifiedWarnings {
		fmt.Fprintf(stderr, "run: verifier result %s — the run is not certified\n", verdict.Result)
		return exitCodeError(1)
	}
	return nil
}

// resolvePlanPath maps a bare plan id to its file in PlansDir; anything that
// looks like a path is used as-is.
func resolvePlanPath(ref string) string {
	if strings.ContainsAny(ref, `/\`) || strings.HasSuffix(ref, ".json") {
		return ref
	}
	return filepath.Join(PlansDir(), ref+".json")
}

// classifyRunOutcome maps how the agent loop ended to the typed run status.
// Order matters: an explicit cancel cause (limit, plan invalidation) outranks
// the generic error the cancelled loop surfaced.
func classifyRunOutcome(ctx context.Context, runErr error, tracker *limits.Tracker) string {
	cause := context.Cause(ctx)
	var lerr *limits.LimitError
	switch {
	case errors.As(cause, &lerr):
		return "limit_exhausted:" + lerr.Limit
	case errors.Is(cause, planguard.ErrPlanInvalidated):
		return "plan_invalidated"
	case errors.Is(cause, context.DeadlineExceeded):
		return "timeout"
	case runErr != nil && errors.Is(runErr, adk.ErrExceedMaxIterations):
		return "max_steps_exhausted"
	}
	if t := tracker.Tripped(); t != nil {
		return "limit_exhausted:" + t.Limit
	}
	if runErr != nil {
		return "provider_error"
	}
	return "completed"
}

// runPrompt embeds the plan's identity and proposals as context; ENFORCEMENT
// lives in the variance guard, not in this prose.
func runPrompt(p plan.Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Execute the approved plan %s (content hash %s).\n\nTask: %s\n", p.PlanID, p.ContentHash, p.Task)
	writeList := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n%s:\n", title)
		for _, it := range items {
			fmt.Fprintf(&b, "- %s\n", it)
		}
	}
	writeList("Proposed actions", p.ProposedActions)
	writeList("Proposed files (the only files you should change)", p.ProposedFiles)
	writeList("Proposed commands", p.ProposedCommands)
	writeList("Acceptance checks (run EACH with run_command after your changes)", p.AcceptanceChecks)
	b.WriteString("\nStay within the plan: out-of-plan writes or commands require operator variance approval and may be refused. When done, reply with a short factual summary of what you changed and the acceptance results.")
	return b.String()
}

// loadRunEvidence re-reads this run's own evidence from disk (not from
// memory) and filters it by the run's correlation id — the receipt is built
// from what was durably recorded, not from what the process remembers.
func loadRunEvidence(evPath, corr string, stderr io.Writer) []evidence.Record {
	records, err := receipt.LoadEvidenceLog(evPath)
	if err != nil {
		fmt.Fprintf(stderr, "warning: evidence log re-read failed (%v); the receipt will carry no evidence summary\n", err)
		return nil
	}
	var out []evidence.Record
	for _, r := range records {
		if r.CorrelationID == corr {
			out = append(out, r)
		}
	}
	return out
}

type evidenceSummary struct {
	commands        []string
	approvals       []string
	policyDecisions []string
	patches         int
}

// summarizeEvidence projects the run's records into receipt fields.
func summarizeEvidence(recs []evidence.Record) evidenceSummary {
	var s evidenceSummary
	for _, r := range recs {
		s.policyDecisions = append(s.policyDecisions,
			fmt.Sprintf("%s %s %s: %s/%s", r.Tool.Name, r.RiskClass, r.Asset, r.Authorization, r.Execution))
		if r.ApprovalID != "" {
			s.approvals = append(s.approvals, r.ApprovalID)
		}
		if r.Execution != "ok" {
			continue
		}
		switch r.Tool.Name {
		case "run_command":
			s.commands = append(s.commands, r.Asset)
		case "apply_patch":
			s.patches++
		}
	}
	return s
}

// acceptanceResults extracts each acceptance check's recorded exit code from
// the run_command evidence ("exit=N" in ExecutionInfo). Only checks that
// actually ran appear; runverify treats missing required checks as
// inconclusive.
func acceptanceResults(recs []evidence.Record, checks []string) map[string]int {
	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	wanted := map[string]string{}
	for _, c := range checks {
		wanted[norm(c)] = c
	}
	out := map[string]int{}
	for _, r := range recs {
		if r.Tool.Name != "run_command" || r.Execution != "ok" {
			continue
		}
		name, ok := wanted[norm(r.Asset)]
		if !ok {
			continue
		}
		var code int
		if _, err := fmt.Sscanf(r.ExecutionInfo, "exit=%d", &code); err == nil {
			out[name] = code
		}
	}
	return out
}

func acceptanceStrings(acceptance map[string]int) []string {
	out := make([]string, 0, len(acceptance))
	for name, code := range acceptance {
		out = append(out, fmt.Sprintf("%s: exit=%d", name, code))
	}
	sort.Strings(out)
	return out
}

// usageMap projects the accumulated provider usage into the receipt's
// free-form usage field. A run whose provider reported nothing gets nil —
// absence is recorded as absence, never fabricated zeros.
func usageMap(u agent.Usage) map[string]any {
	if u.Turns == 0 {
		return nil
	}
	m := map[string]any{
		"prompt_tokens":     u.PromptTokens,
		"completion_tokens": u.CompletionTokens,
		"total_tokens":      u.TotalTokens,
		"model_turns":       u.Turns,
	}
	if u.CachedTokens > 0 {
		m["cached_tokens"] = u.CachedTokens
	}
	if u.ReasoningTokens > 0 {
		m["reasoning_tokens"] = u.ReasoningTokens
	}
	return m
}

func grantedCapabilities(cfg config.Config) []string {
	var out []string
	if cfg.AllowWrites {
		out = append(out, "writes")
	}
	if cfg.AllowExec {
		out = append(out, "exec")
	}
	return out
}
