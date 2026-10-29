//go:build e2e

// Package install verifies fresh installs against pinned server fixtures and
// installs driven by initialized workspaces.
package install

import (
	"maps"
	"slices"
	"testing"

	"github.com/mclucy/lucy/e2e/internal/testkit"
)

func TestInstall(t *testing.T) {
	binary := testkit.Lucy(t)
	for _, id := range slices.Sorted(maps.Keys(installScenarios)) {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			executeScenario(t, binary, id)
		})
	}
}
