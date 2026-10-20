//go:build e2e

// Package compile verifies Lucy's compilation of pinned repository fixtures.
package compile

import (
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/e2e/internal/testkit"
)

func TestCompile(t *testing.T) {
	binary := testkit.Lucy(t)
	repos := filepath.Join(testkit.RepoRoot(t), "e2e", "testdata", "repo")
	for _, id := range fixtureIDs(t, repos) {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			executeCompile(t, binary, repos, id)
		})
	}
}
