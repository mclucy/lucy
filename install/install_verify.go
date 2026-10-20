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
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"gopkg.in/yaml.v3"
)

func validateUnmanagedArtifacts(ctx context.Context, root string, d *lockfile.Document) error {
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
		enabled := len(planned.Requirements) == 0
		for _, r := range planned.Requirements {
			enabled = enabled || r.Enabled
		}
		if !enabled {
			continue
		}
		directory, folder := d.Server.Directory, "mods"
		if planned.Ecosystem == types.EcosystemBukkit {
			folder = "plugins"
		}
		if planned.Environment == types.EnvironmentMCDR {
			directory = d.MCDR.Directory
			folder = "plugins"
		}
		relative := filepath.Join(directory, folder)
		key := string(planned.Environment) + "/" + string(planned.Ecosystem) + "/" + relative
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
			mods, err := artifact.ReadModules(ctx, filepath.Join(root, filename), planned.Ecosystem)
			if err != nil {
				continue
			}
			for _, other := range d.Packages {
				if other.Environment != planned.Environment || other.Ecosystem != planned.Ecosystem {
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
	log.Debug(
		"ambient workspace validated",
		"root", root,
		"directories", len(seenDirectories),
		"packages", len(d.Packages),
	)
	return nil
}

func checkJava(ctx context.Context, minimum int) error {
	raw, err := exec.CommandContext(ctx, "java", "-version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("Java %d or later is required: %w", minimum, err)
	}
	text := string(raw)
	_, after, ok := strings.Cut(text, "\"")
	if !ok {
		return fmt.Errorf("cannot identify Java version: %s", text)
	}
	v, _, _ := strings.Cut(after, "\"")
	majorText, _, _ := strings.Cut(v, ".")
	if majorText == "1" && strings.Contains(v, ".") {
		majorText = strings.Split(v, ".")[1]
	}
	major, err := strconv.Atoi(majorText)
	if err != nil || major < minimum {
		return fmt.Errorf("Java %d or later required, found %s", minimum, v)
	}
	return nil
}
