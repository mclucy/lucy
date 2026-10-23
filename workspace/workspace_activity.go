package workspace

import (
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/workspace/discovery"
	"gopkg.in/yaml.v3"
)

// RunningAt checks the native world session lock without catalogue or app lookup.
func RunningAt(anchor string) bool {
	root := anchor
	if raw, err := os.ReadFile(filepath.Join(anchor, "config.yml")); err == nil {
		var config struct {
			WorkingDirectory string `yaml:"working_directory"`
		}
		if yaml.Unmarshal(raw, &config) == nil && config.WorkingDirectory != "" {
			root = config.WorkingDirectory
			if !filepath.IsAbs(root) {
				root = filepath.Join(anchor, root)
			}
		}
	}
	ws := &Workspace{ServerDir: root}
	ws.Discovery = discovery.Discover(root)
	ws.selectionErr = ws.Discovery.Select(discovery.Request{})
	return ws.Running()
}
