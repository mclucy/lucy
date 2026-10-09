package workspace

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ActiveAt checks the native world session lock without catalogue or app lookup.
func ActiveAt(anchor string) bool {
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
	ws := Workspace{Root: root, Probe: probeDirectory(root)}
	return ws.Active()
}
