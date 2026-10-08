// Command e2e verifies Lucy's CLI against generated sandbox environments.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	flags := flag.NewFlagSet("e2e", flag.ContinueOnError)
	lucy := flags.String("lucy", "dist/lucy", "path to the Lucy binary")
	sandboxes := flags.String("sandboxes", ".sandboxes", "directory containing generated environments")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: go run ./e2e [flags] <environment ids>")
		fmt.Fprintln(flags.Output(), "Environment ids may be comma- or whitespace-separated. Run from the repository root.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	ids := []string{}
	for _, arg := range flags.Args() {
		ids = append(ids, strings.Fields(strings.ReplaceAll(arg, ",", " "))...)
	}
	if len(ids) == 0 {
		flags.Usage()
		return 2
	}
	binary, err := filepath.Abs(*lucy)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve Lucy binary: %v\n", err)
		return 1
	}
	failed := false
	for _, id := range ids {
		expected, ok := probeScenarios[id]
		if !ok {
			fmt.Printf("FAIL %s: no e2e scenario\n", id)
			failed = true
			continue
		}
		dir := filepath.Join(*sandboxes, id)
		status, err := executeStatus(binary, dir)
		if err != nil {
			fmt.Printf("FAIL %s: %v\n", id, err)
			failed = true
			continue
		}
		failures := expected.check(status)
		if len(failures) > 0 {
			fmt.Printf("FAIL %s: %s\n", id, strings.Join(failures, "; "))
			failed = true
			continue
		}
		fmt.Printf("ok   %s\n", id)
	}
	if failed {
		fmt.Println("verification failed")
		return 1
	}
	return 0
}
