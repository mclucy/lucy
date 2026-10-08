// Package testkit provides case selection and result reporting for CLI e2e suites.
package testkit

import (
	"fmt"
	"strings"
	"sync"
)

// ExpandIDs splits arguments on commas and whitespace, preserving order and duplicates.
func ExpandIDs(args []string) []string {
	ids := make([]string, 0, len(args))
	for _, arg := range args {
		for id := range strings.FieldsSeq(strings.ReplaceAll(arg, ",", " ")) {
			ids = append(ids, id)
		}
	}
	return ids
}

// VerifyAll verifies every case, running up to parallel cases at once and treating
// any value below 1 as serial. It reports one line per case to stdout in the order
// the ids were given, so concurrent cases do not scramble the report, and returns
// 1 if any verification fails, otherwise 0. A failing case never stops the others.
func VerifyAll(ids []string, parallel int, verify func(string) error) int {
	if parallel < 1 {
		parallel = 1
	}
	errs := make([]error, len(ids))
	slots := make(chan struct{}, parallel)
	var running sync.WaitGroup
	for i, id := range ids {
		running.Add(1)
		go func() {
			defer running.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			errs[i] = verify(id)
		}()
	}
	running.Wait()
	var failed bool
	for i, id := range ids {
		if err := errs[i]; err != nil {
			fmt.Printf("FAIL %s: %v\n", id, err)
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
