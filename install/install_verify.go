package install

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/types"
	"gopkg.in/yaml.v3"
)

func validateAmbient(ctx context.Context, root string, d *lockfile.Document) error {
	raw, err := os.ReadFile(filepath.Join(root, ".lucy", "install.yaml"))
	owned := map[string]bool{}
	if err == nil {
		var receipt installReceipt
		if err := yaml.Unmarshal(raw, &receipt); err != nil {
			return err
		}
		for _, entry := range receipt.Entries {
			owned[entry.Path] = true
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	seenDirectories := map[string]bool{}
	for _, planned := range d.Packages {
		active := len(planned.Requirements) == 0
		for _, r := range planned.Requirements {
			active = active || r.Enabled
		}
		if !active {
			continue
		}
		directory, folder := d.Server.Directory, "mods"
		if planned.Loader == types.EcoBukkit {
			folder = "plugins"
		}
		if planned.Runtime == "mcdr" {
			directory = d.MCDR.Directory
			folder = "plugins"
		}
		relative := filepath.Join(directory, folder)
		key := planned.Runtime + "/" + string(planned.Loader) + "/" + relative
		if seenDirectories[key] {
			continue
		}
		seenDirectories[key] = true
		entries, err := os.ReadDir(filepath.Join(root, relative))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			filename := filepath.Join(relative, entry.Name())
			if owned[filepath.ToSlash(filename)] {
				continue
			}
			mods, err := artifact.Inspect(ctx, filepath.Join(root, filename), planned.Loader)
			if err != nil {
				continue
			}
			for _, other := range d.Packages {
				if other.Runtime != planned.Runtime || other.Loader != planned.Loader {
					continue
				}
				for _, discovered := range mods {
					for _, selected := range other.Modules {
						if artifact.ModuleProvides(discovered, selected.ID) || artifact.ModuleProvides(selected, discovered.ID) {
							return fmt.Errorf("unmanaged artifact %s conflicts with selected module %s; adopt or move it explicitly", filename, selected.ID)
						}
					}
				}
			}
		}
	}
	return nil
}

func checkJava(ctx context.Context, minimum int) error {
	raw, err := exec.CommandContext(ctx, "java", "-version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("Java %d or later is required: %w", minimum, err)
	}
	text := string(raw)
	start := strings.IndexByte(text, '"')
	if start < 0 {
		return fmt.Errorf("cannot identify Java version: %s", text)
	}
	v := strings.Split(text[start+1:], "\"")[0]
	majorText := strings.Split(v, ".")[0]
	if majorText == "1" && strings.Contains(v, ".") {
		majorText = strings.Split(v, ".")[1]
	}
	major, err := strconv.Atoi(majorText)
	if err != nil || major < minimum {
		return fmt.Errorf("Java %d or later required, found %s", minimum, v)
	}
	return nil
}
