// Command bob is the LEGACY compatibility alias for bob-eino. The bare name
// "bob" collides on PATH with iam-bob-intendant's `bob` bin, so the canonical
// binary is bob-eino (see 000-docs/004-AT-DECR-bob-eino-machine-identity.md).
// This alias is kept so existing invocations keep working: it prints exactly
// one deprecation line to stderr, then runs the identical internal/cli
// implementation. stdout is untouched, so piped/JSON consumers are unaffected.
package main

import (
	"fmt"
	"os"

	"github.com/intent-solutions-io/iam-bob-eino/internal/cli"
)

func main() {
	// One warning per process, stderr only — never stdout, so machine-read
	// output stays byte-identical to bob-eino's.
	fmt.Fprintln(os.Stderr, "warning: the `bob` command is deprecated; use `bob-eino`")
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
