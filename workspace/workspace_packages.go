package workspace

import (
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/log"
)

// discoverPackages inventories the packages under searchPaths and
// mcdrPluginDirs, when the MCDR wrapper exists. Deduplication and
// local-path enrichment follow the PackageIndex.Add policy.
//
// The scan is local-only. It returns the discovered packages together with
// the sorted, deduplicated paths of jar files whose descriptor could not be
// read. Identifying those packages is a separate step.
func discoverPackages(
	searchPaths []string,
	mcdrPluginDirs []string,
) ([]DiscoveredPackage, []string) {
	idx := NewPackageIndex()
	var (
		mu           sync.Mutex
		unidentified []string
	)

	for _, searchPath := range searchPaths {
		jarFiles, err := findJar(searchPath)
		if err != nil {
			log.Warn(err)
			log.Info("cannot read the mod directory")
			continue
		}

		var wg sync.WaitGroup
		for _, jarPath := range jarFiles {
			wg.Go(func() {
				analyzed, err := artifact.ReadInfo(jarPath)

				if err != nil || len(analyzed) == 0 {
					// An unreadable descriptor leaves the package
					// unidentified; the path is retained for a later
					// identify step.
					mu.Lock()
					unidentified = append(unidentified, jarPath)
					mu.Unlock()
					return
				}
				pkgs := artifactInfoToDiscoveredPackage(analyzed)

				mu.Lock()
				idx.Merge(pkgs)
				mu.Unlock()
			})
		}
		wg.Wait()
	}

	for _, dir := range mcdrPluginDirs {
		pluginFiles, err := findFileWithExt([]string{dir}, ".pyz", ".mcdr")
		if err != nil {
			log.Warn(err)
			log.Info("cannot read the MCDR plugin directory")
			continue
		}
		for _, pluginFile := range pluginFiles {
			analyzed, err := artifact.ReadInfo(pluginFile)
			if err == nil && len(analyzed) > 0 {
				pkgs := artifactInfoToDiscoveredPackage(analyzed)
				idx.Merge(pkgs)
			}
		}
	}

	return idx.Packages(), sortUniquePaths(unidentified)
}

// sortUniquePaths returns paths in ascending order without duplicates. A
// nil or single-element input is returned as is.
func sortUniquePaths(paths []string) []string {
	if len(paths) < 2 {
		return paths
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

// findJar lists regular files with a .jar extension directly inside each
// given directory. It does not walk subdirectories. Servers keep their
// runtime jars at shallow locations with known names.
func findJar(dir ...string) ([]string, error) {
	return findFileWithExt(dir, ".jar")
}

func findFileWithExt(dirs []string, ext ...string) ([]string, error) {
	var files []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if slices.Contains(ext, filepath.Ext(entry.Name())) {
				files = append(files, filepath.Join(dir, entry.Name()))
			}
		}
	}
	return files, nil
}

func artifactInfoToDiscoveredPackage(infos []artifact.PackageInfo) []DiscoveredPackage {
	if len(infos) == 0 {
		return nil
	}
	pkgs := make([]DiscoveredPackage, 0, len(infos))
	for _, info := range infos {
		pkg := DiscoveredPackage{
			ID: catalog.VersionedRef{
				Ref:       info.Ref.Ref,
				Ecosystem: info.Ref.Ecosystem,
				Version:   info.Ref.Version,
			},
			Path: info.FilePath,
		}
		if len(info.Dependencies) > 0 {
			deps := make([]catalog.Dependency, 0, len(info.Dependencies))
			for _, dep := range info.Dependencies {
				deps = append(deps, catalog.Dependency{
					ID: catalog.VersionedRef{
						Ref:       dep.Ref.Ref,
						Ecosystem: dep.Ref.Ecosystem,
					},
					Constraint: dep.Constraint,
					Mandatory:  dep.Mandatory,
					Origin:     catalog.NormalizeDependencyOrigin(dep.Origin),
				})
			}
			pkg.Dependencies = deps
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs
}
