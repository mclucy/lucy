package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/receipt"
	"github.com/mclucy/lucy/server"
)

func validateUnmanagedArtifacts(ctx context.Context, root string, d *document.Lock, overwrite bool, logger *log.Logger) error {
	recorded, err := receipt.Read(root)
	owned := map[string]bool{}
	if err == nil {
		for _, entry := range recorded.Entries {
			owned[entry.Path] = true
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	seenDirectories := map[string]bool{}
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
			// ReadDir reports the entry type without following links. Only regular
			// files are opened, so directories, symlinks, FIFOs, sockets and devices
			// are never handed to a blocking or redirecting open.
			if !entry.Type().IsRegular() {
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
			conflict, moduleID := unmanagedConflict(d, &planned, mods)
			if conflict == nil {
				continue
			}
			if overwrite {
				destination, err := packageDestination(d, conflict)
				if err == nil && filepath.Clean(filename) == filepath.Clean(destination) {
					logger.Debug(
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
	logger.Debug(
		"ambient workspace validated",
		"root", root,
		"directories", len(seenDirectories),
		"packages", len(d.Packages),
	)
	return nil
}

// unmanagedConflict reports the selected package whose module an unmanaged
// artifact duplicates. Environment and ecosystem must match, because one
// module identity may exist independently on the server and in MCDR.
// Disabled packages are not selected, so they never report a conflict.
func unmanagedConflict(
	d *document.Lock,
	planned *document.Package,
	discovered []catalog.DescriptorModule,
) (*document.Package, string) {
	for i := range d.Packages {
		other := &d.Packages[i]
		if !other.Enabled() {
			continue
		}
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
