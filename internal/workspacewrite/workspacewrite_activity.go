package workspacewrite

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/workspace"
	"gopkg.in/yaml.v3"
)

// CheckInactive includes both declared runtime locations before replay can
// replace state. Callers name the operation that needs the pause so the error
// tells the user which command the server must stop for.
func CheckInactive(root string, during string) error {
	directories := map[string]bool{root: true}
	for _, name := range []string{document.ManifestFilename, document.LockFilename} {
		if err := SafePath(root, name); err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(root, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		var state struct {
			Server struct {
				Directory string `yaml:"directory"`
			} `yaml:"server"`
		}
		if err := yaml.Unmarshal(data, &state); err != nil {
			return err
		}
		if state.Server.Directory != "" {
			directory := filepath.FromSlash(state.Server.Directory)
			if err := SafePath(root, directory); err != nil {
				return err
			}
			directories[filepath.Join(root, directory)] = true
		}
	}
	for directory := range directories {
		if workspace.RunningAt(directory) {
			return fmt.Errorf("stop the running Minecraft server before %s", during)
		}
	}
	return nil
}
