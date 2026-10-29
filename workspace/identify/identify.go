// Package identify associates local package files with their upstream identities.
package identify

import (
	"fmt"
	"strings"
	"sync"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
)

// Packages adds upstream identities for observed.UnidentifiedPackageFiles.
// It preserves descriptor-derived packages and dependencies, updates the same
// observation, and returns it. Unmatched paths remain. Provider failures
// include file context and are returned even when a later provider identifies
// the file.
func Packages(observed *workspace.Workspace) (*workspace.Workspace, []error) {
	if observed == nil {
		return nil, nil
	}
	candidates := observed.UnidentifiedPackageFiles
	if len(candidates) == 0 {
		return observed, nil
	}

	// Each worker owns one slot. Merge in candidate order after all lookups finish.
	lookups := make([]routing.ArtifactLookup, len(candidates))
	var wg sync.WaitGroup
	for i, path := range candidates {
		wg.Go(func() {
			lookups[i] = routing.ResolveArtifactByHash(artifact.File{Path: path})
		})
	}
	wg.Wait()

	var (
		idx          *workspace.PackageIndex
		unidentified []string
		errs         []error
	)
	for i, lookup := range lookups {
		path := candidates[i]
		if lookup.Matched() {
			if idx == nil {
				idx = workspace.NewPackageIndex()
				idx.Merge(observed.Packages)
			}
			idx.Add(identifiedPackage(path, lookup.Ref))
		} else {
			unidentified = append(unidentified, path)
		}
		for _, providerErr := range lookup.Errors {
			errs = append(errs, fmt.Errorf("%s: %w", path, providerErr))
		}
	}

	if idx != nil {
		observed.Packages = idx.Packages()
	}
	observed.UnidentifiedPackageFiles = unidentified
	return observed, errs
}

func identifiedPackage(
	path string,
	ref catalog.VersionedRef,
) workspace.DiscoveredPackage {
	ecosystem := ref.Ecosystem
	if ecosystem == catalog.EcosystemUnspecified {
		ecosystem = catalog.EcosystemForge
	}

	resolved := ref.Version
	if resolved == "" {
		resolved = version.Unknown
	}

	name := ref.Name
	if ref.Provider == catalog.ProviderMCDR {
		name = catalog.Name(
			strings.ReplaceAll(string(ref.Name), "_", "-"),
		)
	}

	return workspace.DiscoveredPackage{
		ID: catalog.VersionedRef{
			Name:      name,
			Provider:  ref.Provider,
			Ecosystem: ecosystem,
			Version:   resolved,
		},
		Path: path,
	}
}
