// Command init verifies Lucy's server probing against generated environments.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/e2e/internal/testkit"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	flags := flag.NewFlagSet("e2e/init", flag.ContinueOnError)
	lucy := flags.String("lucy", "dist/lucy", "path to the Lucy binary")
	sandboxes := flags.String("sandboxes", ".sandboxes", "directory containing generated environments")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: go run ./e2e/init [flags] scenario ids")
		fmt.Fprintln(flags.Output(), "IDs may be comma- or whitespace-separated.")
		fmt.Fprintln(flags.Output(), "Run from the repository root.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	ids := testkit.ExpandIDs(flags.Args())
	if len(ids) == 0 {
		flags.Usage()
		return 2
	}
	binary, err := filepath.Abs(*lucy)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve Lucy binary: %v\n", err)
		return 1
	}
	return testkit.VerifyAll(ids, func(id string) error {
		expected, ok := probeScenarios[id]
		if !ok {
			return errors.New("no e2e scenario")
		}
		status, err := executeStatus(binary, filepath.Join(*sandboxes, id))
		if err != nil {
			return err
		}
		if failures := expected.check(status); len(failures) > 0 {
			return errors.New(strings.Join(failures, "; "))
		}
		return nil
	})
}
