// The evidence subcommand: inspect and chain-verify the append-only local
// evidence log. All output is content-safe metadata (the records themselves
// never carry content — only hashes, paths, and short summaries).
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/intent-solutions-io/iam-bob-eino/internal/evidence"
	"github.com/intent-solutions-io/iam-bob-eino/internal/receipt"
	"github.com/intent-solutions-io/iam-bob-eino/internal/version"
)

func cmdEvidence(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: bob-eino evidence <list|show <run-id>|verify-chain> [-evidence-dir dir] [-json]")
	}
	sub := args[0]
	fs := flag.NewFlagSet("bob-eino evidence "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	evDir := fs.String("evidence-dir", "", "evidence directory (default: the canonical state dir)")
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON output")

	var runID string
	rest := args[1:]
	if sub == "show" {
		if len(rest) == 0 || len(rest[0]) == 0 || rest[0][0] == '-' {
			return fmt.Errorf("usage: bob-eino evidence show <run-id> [-json]")
		}
		runID, rest = rest[0], rest[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}

	evPath, err := readEvidencePath(*evDir, stderr)
	if err != nil {
		return err
	}

	switch sub {
	case "list":
		return evidenceList(evPath, *jsonOut, stdout, stderr)
	case "show":
		return evidenceShow(evPath, runID, *jsonOut, stdout, stderr)
	case "verify-chain":
		return evidenceVerifyChain(evPath, *jsonOut, stdout, stderr)
	default:
		return fmt.Errorf("evidence: unknown subcommand %q (list | show <run-id> | verify-chain)", sub)
	}
}

// warnUnknownSchemas emits one warning per unknown record schema version.
// Known: the current version and the pre-v2 records that carry no field.
func warnUnknownSchemas(records []evidence.Record, stderr io.Writer) {
	seen := map[string]bool{}
	for _, r := range records {
		if r.SchemaVersion == "" || r.SchemaVersion == version.EvidenceSchemaVersion || seen[r.SchemaVersion] {
			continue
		}
		seen[r.SchemaVersion] = true
		fmt.Fprintf(stderr, "warning: evidence records with unsupported schema_version %q (this build understands %q); shown as-is\n",
			r.SchemaVersion, version.EvidenceSchemaVersion)
	}
}

// runGroup is one correlation-id cluster in the log.
type runGroup struct {
	CorrelationID string `json:"correlation_id"`
	Records       int    `json:"records"`
	FirstAt       string `json:"first_at"`
	LastAt        string `json:"last_at"`
	Denied        int    `json:"denied"`
	Errors        int    `json:"errors"`
}

func evidenceList(evPath string, jsonOut bool, stdout, stderr io.Writer) error {
	records, err := receipt.LoadEvidenceLog(evPath)
	if err != nil {
		return evidenceLoadError(err, stderr)
	}
	warnUnknownSchemas(records, stderr)
	var order []string
	groups := map[string]*runGroup{}
	for _, r := range records {
		g, ok := groups[r.CorrelationID]
		if !ok {
			g = &runGroup{CorrelationID: r.CorrelationID, FirstAt: r.Timestamp}
			groups[r.CorrelationID] = g
			order = append(order, r.CorrelationID)
		}
		g.Records++
		g.LastAt = r.Timestamp
		if r.Authorization == "denied" {
			g.Denied++
		}
		if r.Execution == "error" {
			g.Errors++
		}
	}
	if jsonOut {
		out := make([]runGroup, 0, len(order))
		for _, id := range order {
			out = append(out, *groups[id])
		}
		return writeJSON(stdout, out)
	}
	fmt.Fprintf(stdout, "%-24s %8s %7s %7s  %s\n", "correlation_id", "records", "denied", "errors", "first_at")
	for _, id := range order {
		g := groups[id]
		fmt.Fprintf(stdout, "%-24s %8d %7d %7d  %s\n", g.CorrelationID, g.Records, g.Denied, g.Errors, g.FirstAt)
	}
	return nil
}

func evidenceShow(evPath, runID string, jsonOut bool, stdout, stderr io.Writer) error {
	records, err := receipt.LoadEvidenceLog(evPath)
	if err != nil {
		return evidenceLoadError(err, stderr)
	}
	warnUnknownSchemas(records, stderr)
	var matched []evidence.Record
	for _, r := range records {
		if r.CorrelationID == runID {
			matched = append(matched, r)
		}
	}
	if len(matched) == 0 {
		fmt.Fprintf(stderr, "evidence: no records for correlation id %q\n", runID)
		return exitCodeError(1)
	}
	if jsonOut {
		return writeJSON(stdout, matched)
	}
	for _, r := range matched {
		fmt.Fprintf(stdout, "%s  %-12s %-4s %-30s auth=%-7s exec=%-7s verified=%s\n",
			r.Timestamp, r.Tool.Name, r.RiskClass, truncate(r.Asset, 30), r.Authorization, r.Execution, r.Verified)
	}
	return nil
}

func evidenceVerifyChain(evPath string, jsonOut bool, stdout, stderr io.Writer) error {
	broken, err := receipt.VerifyChainFromFile(evPath)
	if err != nil {
		return evidenceLoadError(err, stderr)
	}
	intact := broken == -1
	if jsonOut {
		if werr := writeJSON(stdout, map[string]any{"intact": intact, "broken_at": broken, "path": evPath}); werr != nil {
			return werr
		}
	} else if intact {
		fmt.Fprintf(stdout, "chain intact: %s\n", evPath)
	} else {
		fmt.Fprintf(stdout, "chain BROKEN at record %d: %s\n", broken, evPath)
	}
	if !intact {
		return exitCodeError(1)
	}
	return nil
}

// evidenceLoadError surfaces a malformed-line error with its line number and
// exits 1; other read errors pass through as ordinary errors.
func evidenceLoadError(err error, stderr io.Writer) error {
	var mle *receipt.MalformedLineError
	if errors.As(err, &mle) {
		fmt.Fprintf(stderr, "evidence: %v\n", mle)
		return exitCodeError(1)
	}
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
