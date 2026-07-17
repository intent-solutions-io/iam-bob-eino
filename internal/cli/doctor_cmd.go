// The doctor subcommand: preflight the environment for the plan/run/verify
// lifecycle. Human-readable rows by default, -json for machines; exit is
// non-zero when any REQUIRED check fails.
package cli

import (
	"fmt"
	"io"

	"github.com/intent-solutions-io/iam-bob-eino/internal/doctor"
)

func cmdDoctor(args []string, stdout, stderr io.Writer) error {
	c := newCommonFlags("doctor", stderr)
	netCheck := c.fs.Bool("net", false, "probe network reachability of the provider endpoint")
	if err := c.fs.Parse(args); err != nil {
		return err
	}
	cfg, err := c.buildConfig(stderr)
	if err != nil {
		// Doctor must still run on a broken config — that is exactly when an
		// operator needs it. Report the config error as context and continue
		// with whatever merged (a zero config yields honest FAILs).
		fmt.Fprintf(stderr, "note: config did not validate (%v); running checks anyway\n", err)
	}

	checks := doctor.Run(doctor.Options{
		Cfg:      cfg,
		Network:  *netCheck,
		StateDir: StateDir(),
	})

	if c.jsonOut {
		if werr := writeJSON(stdout, map[string]any{
			"checks":           checks,
			"required_failure": doctor.HasRequiredFailure(checks),
		}); werr != nil {
			return werr
		}
	} else {
		for _, chk := range checks {
			req := " "
			if chk.Required {
				req = "*"
			}
			fmt.Fprintf(stdout, "%-7s %s %-24s %s\n", chk.Status, req, chk.Name, chk.Detail)
		}
	}

	if doctor.HasRequiredFailure(checks) {
		fmt.Fprintln(stderr, "doctor: required checks failed")
		return exitCodeError(1)
	}
	return nil
}
