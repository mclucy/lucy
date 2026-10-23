package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/workspace/discovery"
)

func TestObserveIgnoresJarNamedDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "plugins.jar"), 0o755); err != nil {
		t.Fatalf("create jar-named directory: %v", err)
	}

	observed := Observe(root, discovery.Request{})
	if observed.Runtime() != nil {
		t.Fatalf("observation selected %#v, want no runtime", observed.Runtime())
	}
}
