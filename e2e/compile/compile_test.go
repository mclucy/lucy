//go:build e2e

// Package compile verifies Lucy's compilation of pinned repository fixtures.
//
// LUCY_COMPILE_LAYER=smoke limits the table to fixtures marked smoke: true.
package compile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/e2e/internal/testkit"
)

func TestCompile(t *testing.T) {
	binary := testkit.Lucy(t)
	repos := filepath.Join(testkit.RepoRoot(t), "e2e", "testdata", "repo")
	smokeOnly := false
	switch layer := os.Getenv("LUCY_COMPILE_LAYER"); layer {
	case "", "full":
	case "smoke":
		smokeOnly = true
	default:
		t.Fatalf("unknown LUCY_COMPILE_LAYER %q", layer)
	}
	fixtures := selectFixtures(t, repos, smokeOnly)
	if len(fixtures) == 0 {
		t.Fatal("no compile fixtures selected")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.ID, func(t *testing.T) {
			t.Parallel()
			executeCompile(t, binary, fixture)
		})
	}
}
