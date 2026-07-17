// Temporary stubs for subcommands whose implementation lands in follow-up
// commits of the lifecycle build. Each returns an honest error instead of
// silently falling through to the flat one-shot form (which would otherwise
// treat "doctor" as a task for the model).
package cli

import (
	"fmt"
	"io"
)

func cmdDoctor(_ []string, _, _ io.Writer) error {
	return fmt.Errorf("bob-eino doctor: not implemented yet in this build")
}

func cmdPlan(_ []string, _ io.Reader, _, _ io.Writer) error {
	return fmt.Errorf("bob-eino plan: not implemented yet in this build")
}

func cmdRun(_ []string, _ io.Reader, _, _ io.Writer) error {
	return fmt.Errorf("bob-eino run: not implemented yet in this build")
}

func cmdVerify(_ []string, _, _ io.Writer) error {
	return fmt.Errorf("bob-eino verify: not implemented yet in this build")
}

func cmdEvidence(_ []string, _, _ io.Writer) error {
	return fmt.Errorf("bob-eino evidence: not implemented yet in this build")
}
