// The verify subcommand: re-verify a sealed run receipt after the fact —
// tamper-rejecting receipt load, the run's evidence re-filtered by run id,
// the model-free verifier re-run against live git state. Exit 0 only when
// the re-derived verdict is verified or verified_with_warnings.
package cli

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/intent-solutions-io/iam-bob-eino/internal/gitstate"
	"github.com/intent-solutions-io/iam-bob-eino/internal/plan"
	"github.com/intent-solutions-io/iam-bob-eino/internal/receipt"
	"github.com/intent-solutions-io/iam-bob-eino/internal/runverify"
)

func cmdVerify(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("bob-eino verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	receiptRef := fs.String("receipt", "", "receipt file path or run id (required)")
	planRef := fs.String("plan", "", "optional plan id or path to re-check proposals against")
	evDir := fs.String("evidence-dir", "", "evidence directory (default: the canonical state dir)")
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *receiptRef == "" {
		return fmt.Errorf("usage: bob-eino verify -receipt <path|run-id> [-plan <id|path>] [-json]")
	}

	r, err := receipt.Load(resolveReceiptPath(*receiptRef))
	if err != nil {
		return err // tamper-rejecting: an edited receipt never verifies
	}

	// Optional plan cross-check: the plan on disk must be the exact plan the
	// receipt claims to have executed.
	var proposedFiles []string
	var requiredChecks []string
	if *planRef != "" {
		p, perr := plan.Load(resolvePlanPath(*planRef))
		if perr != nil {
			return perr
		}
		if p.PlanID != r.PlanID || p.ContentHash != r.PlanHash {
			return fmt.Errorf("verify: plan %s (hash %s) is not the plan this receipt executed (%s, hash %s)",
				p.PlanID, p.ContentHash, r.PlanID, r.PlanHash)
		}
		proposedFiles = p.ProposedFiles
		requiredChecks = p.AcceptanceChecks
	} else {
		// Without the plan, the receipt's own recorded change set is the
		// baseline: verification is then internal-consistency + evidence +
		// git, not plan conformance.
		proposedFiles = r.FilesChanged
	}

	evPath, err := readEvidencePath(*evDir, stderr)
	if err != nil {
		return err
	}
	records, err := receipt.LoadEvidenceLog(evPath)
	if err != nil {
		return err // MalformedLineError carries the line number
	}
	runRecords := records[:0:0]
	for _, rec := range records {
		if rec.CorrelationID == r.RunID {
			runRecords = append(runRecords, rec)
		}
	}

	in := runverify.Input{
		WorkspaceRoot: r.WorkspaceIdentity,
		Plan: runverify.Plan{
			WorkspaceRoot: r.WorkspaceIdentity,
			ProposedFiles: proposedFiles,
			StartSHA:      r.WorkspaceStartSHA,
			EndSHA:        r.WorkspaceEndSHA,
		},
		Evidence:       runRecords,
		ChangedFiles:   r.FilesChanged,
		Acceptance:     acceptanceFromReceipt(r),
		RequiredChecks: requiredChecks,
		AgentClaim:     r.AgentClaim,
	}
	// Live git re-check: the workspace must still be at the recorded end SHA.
	if r.WorkspaceEndSHA != "" {
		root := r.WorkspaceIdentity
		in.GitState = func() (string, error) {
			st, gerr := gitstate.Head(root)
			return st.HeadSHA, gerr
		}
	}
	verdict := runverify.Verify(in)

	ok := verdict.Result == runverify.ResultVerified || verdict.Result == runverify.ResultVerifiedWarnings
	if *jsonOut {
		if werr := writeJSON(stdout, map[string]any{
			"run_id":   r.RunID,
			"plan_id":  r.PlanID,
			"result":   verdict.Result,
			"checks":   verdict.Checks,
			"failures": verdict.Failures,
			"warnings": verdict.Warnings,
			"verified": ok,
		}); werr != nil {
			return werr
		}
	} else {
		fmt.Fprintf(stdout, "run_id: %s\nplan_id: %s\n", r.RunID, r.PlanID)
		names := make([]string, 0, len(verdict.Checks))
		for name := range verdict.Checks {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(stdout, "%-8s %s\n", verdict.Checks[name], name)
		}
		for _, f := range verdict.Failures {
			fmt.Fprintf(stdout, "failure: %s\n", f)
		}
		for _, w := range verdict.Warnings {
			fmt.Fprintf(stdout, "warning: %s\n", w)
		}
		fmt.Fprintf(stdout, "result: %s\n", verdict.Result)
	}
	if !ok {
		return exitCodeError(1)
	}
	return nil
}

// resolveReceiptPath maps a bare run id to its receipt file in ReceiptsDir.
func resolveReceiptPath(ref string) string {
	if strings.ContainsAny(ref, `/\`) || strings.HasSuffix(ref, ".json") {
		return ref
	}
	return filepath.Join(ReceiptsDir(), ref+".receipt.json")
}

// acceptanceFromReceipt parses the receipt's recorded "name: exit=N" test
// results back into the verifier's acceptance map.
func acceptanceFromReceipt(r receipt.RunReceipt) map[string]int {
	out := map[string]int{}
	for _, tr := range r.TestResults {
		i := strings.LastIndex(tr, ": exit=")
		if i < 0 {
			continue
		}
		code, err := strconv.Atoi(strings.TrimSpace(tr[i+len(": exit="):]))
		if err != nil {
			continue
		}
		out[tr[:i]] = code
	}
	return out
}
