// Command compile verifies Lucy's compilation of pinned repository fixtures.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/e2e/internal/testkit"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	flags := flag.NewFlagSet("e2e/compile", flag.ContinueOnError)
	lucy := flags.String("lucy", "dist/lucy", "path to the Lucy binary")
	repos := flags.String("repos", "e2e/testdata/repo", "directory containing compile fixture manifests")
	list := flags.Bool("list", false, "print every fixture id and exit")
	parallel := flags.Int("p", 1, "run up to N fixtures concurrently")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: go run ./e2e/compile [flags] [fixture ids]")
		fmt.Fprintln(flags.Output(), "IDs may be comma- or whitespace-separated. Defaults to all fixtures.")
		fmt.Fprintln(flags.Output(), "Without ids every fixture runs, one at a time unless -p allows more.")
		fmt.Fprintln(flags.Output(), "Run from the repository root.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *list {
		ids, err := fixtureIDs(*repos)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		for _, id := range ids {
			fmt.Println(id)
		}
		return 0
	}
	binary, err := filepath.Abs(*lucy)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve Lucy binary: %v\n", err)
		return 1
	}
	return runCompile(binary, *repos, testkit.ExpandIDs(flags.Args()), *parallel)
}
