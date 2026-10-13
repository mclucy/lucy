// Command lock verifies fresh installs against pinned server fixtures.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/mclucy/lucy/e2e/internal/testkit"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	flags := flag.NewFlagSet("e2e/lock", flag.ContinueOnError)
	lucy := flags.String("lucy", "dist/lucy", "path to the Lucy binary")
	sandboxes := flags.String("sandboxes", ".sandboxes", "directory containing generated environments")
	list := flags.Bool("list", false, "print every scenario id and exit")
	parallel := flags.Int("p", 1, "run up to N scenarios concurrently")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: go run ./e2e/lock [flags] [scenario ids]")
		fmt.Fprintln(flags.Output(), "IDs may be comma- or whitespace-separated. Defaults to all scenarios.")
		fmt.Fprintln(flags.Output(), "Run from the repository root after building Lucy and generating the fixture environments.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	ids := testkit.ExpandIDs(flags.Args())
	if *list || len(ids) == 0 {
		ids = slices.Sorted(maps.Keys(lockScenarios))
	}
	if *list {
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
	return testkit.VerifyAll(ids, *parallel, func(id string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		return executeScenario(ctx, binary, *sandboxes, id)
	})
}
