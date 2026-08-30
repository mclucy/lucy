package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProbeDirectoryIgnoresJarNamedDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "plugins.jar"), 0o755); err != nil {
		t.Fatalf("create jar-named directory: %v", err)
	}

	probe := probeDirectory(root)
	if !probe.IsEmpty() {
		t.Fatalf("probe with jar-named directory = %#v, want empty", probe)
	}
}
