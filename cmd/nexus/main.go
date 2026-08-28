// Command nexus is Project Nexus's CLI: the bicameral Code/Explainer branch
// workflow.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/sunprema/nexus-cli/internal/cli"
)

type silentError interface {
	error
	AlreadyPrinted() bool
}

func main() {
	root := cli.NewRootCmd()
	if err := root.ExecuteContext(context.Background()); err != nil {
		var silent silentError
		if !errors.As(err, &silent) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
