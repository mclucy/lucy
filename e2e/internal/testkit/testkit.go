// Package testkit provides case selection and result reporting for CLI e2e suites.
package testkit

import (
	"fmt"
	"strings"
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

// VerifyAll reports every case to stdout and returns 1 if any verification fails, otherwise 0.
func VerifyAll(ids []string, verify func(string) error) int {
	var failed bool
	for _, id := range ids {
		if err := verify(id); err != nil {
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
