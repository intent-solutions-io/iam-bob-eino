// The plan subcommand: run the model with READ-ONLY tools against the
// workspace and produce a hashed, validated plan artifact (internal/plan)
// saved outside the workspace. A plan is advisory ("local_untrusted"); it
// grants nothing — run enforces it later through the variance guard.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	einomodel "github.com/cloudwego/eino/components/model"

	"github.com/intent-solutions-io/iam-bob-eino/internal/agent"
	"github.com/intent-solutions-io/iam-bob-eino/internal/config"
	"github.com/intent-solutions-io/iam-bob-eino/internal/gitstate"
	"github.com/intent-solutions-io/iam-bob-eino/internal/governor"
	"github.com/intent-solutions-io/iam-bob-eino/internal/plan"
	"github.com/intent-solutions-io/iam-bob-eino/internal/policy"
	"github.com/intent-solutions-io/iam-bob-eino/internal/provider"
	"github.com/intent-solutions-io/iam-bob-eino/internal/tools"
	"github.com/intent-solutions-io/iam-bob-eino/internal/version"
	"github.com/intent-solutions-io/iam-bob-eino/internal/workspace"
)

// ErrPlanDraft marks a model plan draft that failed strict parsing or carried
// unknown capabilities. Nothing is written when this is returned.
var ErrPlanDraft = errors.New("plan: model draft is not a valid plan JSON object")

// Test seams: the model factory and the clock are vars so the offline
// deterministic lifecycle tests can substitute a scripted model fixture and a
// fixed time without touching the network or the wall clock.
var (
	modelFactory = func(ctx context.Context, cfg config.Config) (einomodel.ToolCallingChatModel, error) {
		pc, err := provider.FromConfig(cfg.Provider, cfg.Model, cfg.BaseURL)
		if err != nil {
			return nil, err
		}
		return provider.New(ctx, pc)
	}
	now = time.Now
)

func cmdPlan(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	c := newCommonFlags("plan", stderr)
	if err := c.fs.Parse(args); err != nil {
		return err
	}
	task := strings.TrimSpace(strings.Join(c.fs.Args(), " "))
	if task == "" {
		return fmt.Errorf("usage: bob-eino plan [flags] <task>")
	}
	cfg, err := c.buildConfig(stderr)
	if err != nil {
		return err
	}
	if cfg.Workspace == "" {
		cfg.Workspace = "."
	}

	ws, err := workspace.New(cfg.Workspace)
	if err != nil {
		return err
	}
	defer ws.Close()

	var startSHA, branch string
	if st, gerr := gitstate.Head(ws.Root()); gerr != nil {
		fmt.Fprintf(stderr, "warning: %v; the plan will not be SHA-pinned and the run-time HEAD check will be skipped\n", gerr)
	} else {
		startSHA, branch = st.HeadSHA, st.Branch
		if st.Dirty {
			fmt.Fprintf(stderr, "warning: workspace has uncommitted changes on %s; the plan pins the current HEAD, not the dirty tree\n", branch)
		}
	}

	sink, evPath, err := openEvidenceSink(cfg, stderr)
	if err != nil {
		return err
	}
	defer sink.Close()

	// Planning is read-only BY CONSTRUCTION: default policy (no capabilities,
	// regardless of -allow-* flags) and only the read-only tool set — the
	// mutation tool builders are never constructed.
	gov := governor.New(ws, policy.Default(), c.approver(cfg, stdin, stderr), sink)
	toolset, err := tools.ReadOnly(gov)
	if err != nil {
		return err
	}

	ctx, _, stop := runContext(cfg)
	defer stop()

	model, err := modelFactory(ctx, cfg)
	if err != nil {
		return err
	}
	ag, err := agent.New(ctx, agent.Config{Model: model, Tools: toolset, MaxIterations: cfg.MaxSteps})
	if err != nil {
		return err
	}

	fmt.Fprintf(stderr, "%s plan: workspace=%s model=%s/%s tools=read-only evidence=%s\n",
		version.Component, ws.Root(), cfg.Provider, cfg.Model, evPath)

	answer, err := agent.Run(ctx, ag, planningPrompt(task), stderr)
	if err != nil {
		return fmt.Errorf("planning run: %w", err)
	}

	draft, err := parsePlanDraft(answer)
	if err != nil {
		return err
	}

	p := plan.Plan{
		SchemaVersion:        plan.SchemaVersion,
		Task:                 task,
		WorkspaceIdentity:    ws.Root(),
		WorkspaceStartSHA:    startSHA,
		Provider:             cfg.Provider,
		Model:                cfg.Model,
		CreatedAt:            now().UTC().Format(time.RFC3339),
		ProposedActions:      draft.ProposedActions,
		ProposedFiles:        draft.ProposedFiles,
		ProposedCommands:     draft.ProposedCommands,
		RequiredCapabilities: requiredCapabilities(draft),
		AcceptanceChecks:     draft.AcceptanceChecks,
		Risks:                draft.Risks,
		Assumptions:          draft.Assumptions,
		Questions:            draft.Questions,
		Status:               "proposed",
		Authority:            plan.AuthorityLocalUntrusted,
	}
	p.Finalize()

	path, err := plan.Save(p, PlansDir())
	if err != nil {
		return fmt.Errorf("plan rejected, nothing saved: %w", err)
	}

	if c.jsonOut {
		return writeJSON(stdout, map[string]any{
			"plan_id":               p.PlanID,
			"path":                  path,
			"content_hash":          p.ContentHash,
			"workspace":             p.WorkspaceIdentity,
			"workspace_start_sha":   p.WorkspaceStartSHA,
			"branch":                branch,
			"required_capabilities": p.RequiredCapabilities,
			"questions":             p.Questions,
		})
	}
	fmt.Fprintf(stdout, "plan_id: %s\npath: %s\n", p.PlanID, path)
	if len(p.RequiredCapabilities) > 0 {
		fmt.Fprintf(stdout, "required_capabilities: %s\n", strings.Join(p.RequiredCapabilities, ", "))
	}
	for _, q := range p.Questions {
		fmt.Fprintf(stdout, "question: %s\n", q)
	}
	return nil
}

// planningPrompt wraps the task in the planning contract: read-only
// exploration, then exactly one JSON draft object.
func planningPrompt(task string) string {
	return task + `

You are in PLANNING mode. Your tools are read-only (read_file, list_dir,
search_code); explore the workspace as needed, then respond with EXACTLY ONE
JSON object and no other prose, matching:

{"proposed_actions": ["short imperative steps"],
 "proposed_files": ["workspace-relative paths you would create or modify"],
 "proposed_commands": ["shell-free commands you would run"],
 "required_capabilities": ["writes" and/or "exec", only what the plan needs"],
 "acceptance_checks": ["shell-free commands that prove the task is done (at least one)"],
 "risks": ["what could go wrong"],
 "assumptions": ["what you assumed"],
 "questions": ["what you would ask the operator, if anything"]}

Commands (proposed_commands and acceptance_checks) must start with one of:
go, make, pytest, npm, pnpm, cargo, git — and contain no shell metacharacters.`
}

// planDraft is the strict shape of the model's planning answer.
type planDraft struct {
	ProposedActions      []string `json:"proposed_actions"`
	ProposedFiles        []string `json:"proposed_files"`
	ProposedCommands     []string `json:"proposed_commands"`
	RequiredCapabilities []string `json:"required_capabilities"`
	AcceptanceChecks     []string `json:"acceptance_checks"`
	Risks                []string `json:"risks"`
	Assumptions          []string `json:"assumptions"`
	Questions            []string `json:"questions"`
}

// thinkBlockRe matches the <think>…</think> reasoning blocks MiniMax-M3 (the
// documented default model) interleaves with its answer. They can contain
// braces and pseudo-JSON, so they are removed before draft extraction —
// found live in the v0.1.0-rc.1 operational soak, where a brace inside a
// think block corrupted the naive first-to-last-brace span.
var thinkBlockRe = regexp.MustCompile(`(?is)<think>.*?</think>`)

// parsePlanDraft strictly parses the model's final answer into a draft.
// Extraction is DECODE-DRIVEN, not span-guessing: every '{' offset is tried
// newest-first (the draft conventionally ends the answer) and the JSON
// decoder itself decides where a value ends, so unbalanced quotes or braces
// in surrounding prose can never corrupt the scan. The raw answer is tried
// FIRST — a legitimate draft whose strings contain literal think-tags parses
// intact — and only if that fails are <think> reasoning blocks stripped and
// extraction retried. A winning candidate must strictly decode AND carry at
// least one acceptance check, so a stray "{}" in prose can never outrank the
// real draft. Any deviation returns a typed ErrPlanDraft; nothing is written.
func parsePlanDraft(answer string) (planDraft, error) {
	raw := strings.TrimSpace(answer)
	if d, err := extractDraft(raw); err == nil {
		return d, nil
	}
	stripped := strings.TrimSpace(thinkBlockRe.ReplaceAllString(raw, ""))
	d, err := extractDraft(stripped)
	if err != nil {
		return planDraft{}, fmt.Errorf("%w: %v", ErrPlanDraft, err)
	}
	return d, nil
}

// extractDraft tries to decode a draft starting at each '{' in raw, newest
// first, and returns the first candidate that satisfies decodeDraft. The
// reported error is the newest candidate's failure (the most likely "almost
// a draft" object).
func extractDraft(raw string) (planDraft, error) {
	// Tolerate a fenced code block around the object, nothing else.
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
		raw = strings.TrimSpace(raw)
	}
	firstErr := fmt.Errorf("no JSON object in the model answer")
	seen := false
	for i := strings.LastIndexByte(raw, '{'); i >= 0; i = strings.LastIndexByte(raw[:i], '{') {
		d, err := decodeDraft(raw[i:])
		if err == nil {
			return d, nil
		}
		if !seen {
			firstErr, seen = err, true
		}
	}
	return planDraft{}, firstErr
}

// decodeDraft strictly decodes exactly one JSON value from the start of raw
// (trailing prose after the value is expected and ignored — the decoder
// consumed a complete object). A draft without any acceptance check is
// rejected here so empty or unrelated objects fall through to the next
// candidate.
func decodeDraft(raw string) (planDraft, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var d planDraft
	if err := dec.Decode(&d); err != nil {
		return planDraft{}, err
	}
	if len(d.AcceptanceChecks) == 0 {
		return planDraft{}, fmt.Errorf("draft object has no acceptance_checks")
	}
	for _, cap := range d.RequiredCapabilities {
		if cap != "writes" && cap != "exec" {
			return planDraft{}, fmt.Errorf("unknown capability %q (only \"writes\" and \"exec\" exist)", cap)
		}
	}
	return d, nil
}

// requiredCapabilities merges what the model declared with what its own
// proposals imply: proposed file changes imply "writes"; proposed or
// acceptance commands imply "exec" (acceptance checks execute during run).
// The union is sorted for deterministic plan hashing.
func requiredCapabilities(d planDraft) []string {
	set := map[string]bool{}
	for _, c := range d.RequiredCapabilities {
		set[c] = true
	}
	if len(d.ProposedFiles) > 0 {
		set["writes"] = true
	}
	if len(d.ProposedCommands) > 0 || len(d.AcceptanceChecks) > 0 {
		set["exec"] = true
	}
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}
