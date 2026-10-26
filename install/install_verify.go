package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/server"
	"gopkg.in/yaml.v3"
)

func validateUnmanagedArtifacts(ctx context.Context, root string, d *document.Lock, overwrite bool) error {
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
	lockedDestinations := lockedPackageDestinations(d)
	for _, planned := range d.Packages {
		if !planned.Enabled() {
			continue
		}
		directory, folder := d.Server.Directory, packageFolder(planned.Environment, planned.Ecosystem)
		if planned.Environment == server.EnvironmentMCDR {
			directory = d.MCDR.Directory
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
			if expected, ok := lockedDestinations[filepath.Clean(filename)]; ok {
				if _, err := DigestFile(filepath.Join(root, filename), expected); err == nil {
					log.Debug(
						"locked package already occupies its destination",
						"path", filename,
					)
					continue
				}
			}
			mods, err := artifact.ReadModules(ctx, filepath.Join(root, filename), planned.Ecosystem)
			if err != nil {
				continue
			}
			conflict, moduleID := unmanagedConflict(d, &planned, mods)
			if conflict == nil {
				continue
			}
			if overwrite {
				destination, err := packageDestination(d, conflict)
				if err == nil && filepath.Clean(filename) == filepath.Clean(destination) {
					log.Debug(
						"overwriting unmanaged artifact",
						"path", filename,
						"module", moduleID,
					)
					continue
				}
			}
			return fmt.Errorf("unmanaged artifact %s conflicts with selected module %s; adopt or move it explicitly", filename, moduleID)
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

// lockedPackageDestinations maps every enabled package destination to the
// digests the lock pins for it. A destination that already holds those bytes
// belongs to the locked installation even when no receipt records it, which is
// the state initialization leaves behind.
func lockedPackageDestinations(d *document.Lock) map[string]map[string]string {
	destinations := make(map[string]map[string]string, len(d.Packages))
	for i := range d.Packages {
		p := &d.Packages[i]
		if !p.Enabled() {
			continue
		}
		destination, err := packageDestination(d, p)
		if err != nil {
			continue
		}
		destinations[filepath.Clean(destination)] = p.Artifact.Hashes
	}
	return destinations
}

// unmanagedConflict reports the selected package whose module an unmanaged
// artifact duplicates. Environment and ecosystem must match, because one
// module identity may exist independently on the server and in MCDR.
func unmanagedConflict(
	d *document.Lock,
	planned *document.Package,
	discovered []catalog.DescriptorModule,
) (*document.Package, string) {
	for i := range d.Packages {
		other := &d.Packages[i]
		if other.Environment != planned.Environment || other.Ecosystem != planned.Ecosystem {
			continue
		}
		for _, found := range discovered {
			for _, selected := range other.Modules {
				if artifact.ModuleProvides(found, selected.ID) || artifact.ModuleProvides(selected, found.ID) {
					return other, selected.ID
				}
			}
		}
	}
	return nil, ""
}
