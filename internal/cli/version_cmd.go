// The version subcommand: identity, build provenance, and schema versions.
// Richer than the flat form's -version flag (which is frozen for
// compatibility); all identity strings come from internal/identity and
// internal/version — never hand-built here.
package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/intent-solutions-io/iam-bob-eino/internal/identity"
	"github.com/intent-solutions-io/iam-bob-eino/internal/plan"
	"github.com/intent-solutions-io/iam-bob-eino/internal/receipt"
	"github.com/intent-solutions-io/iam-bob-eino/internal/version"
)

func cmdVersion(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("bob-eino version", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *jsonOut {
		// Deliberately no per-process instance id here: the JSON form is for
		// machine consumers and stays deterministic for a given build.
		return writeJSON(stdout, map[string]any{
			"component":               identity.ComponentID,
			"implementation":          identity.ImplementationID,
			"agent":                   identity.AgentID,
			"runtime":                 identity.RuntimeID,
			"persona":                 identity.PersonaID,
			"version":                 version.AgentVersion,
			"build_commit":            version.BuildCommit,
			"build_date":              version.BuildDate,
			"go_version":              version.GoVersion(),
			"engine":                  version.Engine,
			"engine_version":          version.EinoVersion(),
			"identity_schema_version": version.IdentitySchemaVersion,
			"evidence_schema_version": version.EvidenceSchemaVersion,
			"receipt_schema_version":  receipt.SchemaVersion,
			"plan_schema_version":     plan.SchemaVersion,
		})
	}

	id, err := identity.New(identity.RoleCoding, "local", version.AgentVersion)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, id.Display())
	fmt.Fprintf(stdout, "  build:     %s (%s)\n", version.BuildCommit, version.BuildDate)
	fmt.Fprintf(stdout, "  go:        %s\n", version.GoVersion())
	fmt.Fprintf(stdout, "  engine:    %s %s\n", version.Engine, version.EinoVersion())
	fmt.Fprintf(stdout, "  schemas:   %s | %s | plan schema %s\n",
		version.EvidenceSchemaVersion, receipt.SchemaVersion, plan.SchemaVersion)
	return nil
}
