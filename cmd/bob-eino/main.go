// Command bob-eino is the CANONICAL CLI entry point for the Eino/Go runtime of
// the Intent Agent Model coding agent ("Bob" is the persona; intent-bob-eino
// is the component). The whole implementation lives in internal/cli so the
// legacy `bob` alias cannot drift from this binary.
package main

import (
	"os"

	"github.com/intent-solutions-io/iam-bob-eino/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
