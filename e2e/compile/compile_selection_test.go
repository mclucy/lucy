package compile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSelectFixturesHonorsSmokeLayer(t *testing.T) {
	t.Parallel()
	repos := t.TempDir()
	fixtures := map[string]string{
		"smoke-a": "url: https://example.invalid/a.git\ncommit: " + strings.Repeat("a", 40) + "\nsmoke: true\n",
		"smoke-b": "url: https://example.invalid/b.git\ncommit: " + strings.Repeat("b", 40) + "\nsmoke: true\n",
		"full-c":  "url: https://example.invalid/c.git\ncommit: " + strings.Repeat("c", 40) + "\n",
	}
	for id, content := range fixtures {
		if err := os.WriteFile(filepath.Join(repos, id+".yaml"), []byte(content), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", id, err)
		}
	}
	selectedIDs := func(selected []compileFixture) []string {
		ids := make([]string, 0, len(selected))
		for _, fixture := range selected {
			ids = append(ids, fixture.ID)
		}
		return ids
	}
	smoke := selectedIDs(selectFixtures(t, repos, true))
	if !slices.Equal(smoke, []string{"smoke-a", "smoke-b"}) {
		t.Errorf("smoke layer selected %v", smoke)
	}
	full := selectedIDs(selectFixtures(t, repos, false))
	if !slices.Equal(full, []string{"full-c", "smoke-a", "smoke-b"}) {
		t.Errorf("full layer selected %v", full)
	}
}
