//go:build e2e

// Package status verifies Lucy's server probing against generated sandboxes.
package status

import (
	"maps"
	"slices"
	"testing"

	"github.com/mclucy/lucy/e2e/internal/testkit"
)

func TestProbe(t *testing.T) {
	binary := testkit.Lucy(t)
	for _, id := range slices.Sorted(maps.Keys(probeScenarios)) {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			want := probeScenarios[id]
			status, err := executeStatus(binary, testkit.Sandbox(t, id))
			if err != nil {
				t.Fatal(err)
			}
			want.check(t, status)
		})
	}
}
