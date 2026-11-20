// Package identify associates local package files with their upstream identities.
package identify

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
)

// HashLookup resolves artifact fingerprints through the configured upstream
// catalogue, returning one resolution per fingerprint in input order. Callers
// inject the routing capability they own.
type HashLookup func(
	ctx context.Context,
	artifacts []upstream.ArtifactFingerprint,
) []upstream.ArtifactReleaseResolution

// Packages adds upstream identities for observed.UnidentifiedPackageFiles.
// A non-nil resolutions map supplies and retains lookup outcomes by path,
// including misses and provider errors. Unmatched paths remain unidentified.
func Packages(
	ctx context.Context,
	observed *workspace.Workspace,
	lookup HashLookup,
	resolutions map[string]upstream.ArtifactReleaseResolution,
) []error {
	if observed == nil || len(observed.UnidentifiedPackageFiles) == 0 {
		return nil
	}
	candidates := observed.UnidentifiedPackageFiles

	var fingerprints []upstream.ArtifactFingerprint
	for _, path := range candidates {
		if _, resolved := resolutions[path]; !resolved {
			fingerprints = append(fingerprints, observed.PackageAnalysis(path))
		}
	}
	if len(fingerprints) > 0 && lookup == nil {
		return []error{errors.New("package identification requires a hash lookup")}
	}

	var lookups []upstream.ArtifactReleaseResolution
	if len(fingerprints) > 0 {
		lookups = lookup(ctx, fingerprints)
	}

	var (
		idx          *workspace.PackageIndex
		unidentified []string
		errs         []error
	)
	next := 0
	for _, path := range candidates {
		resolution, resolved := resolutions[path]
		if !resolved {
			resolution = lookups[next]
			next++
			if resolutions != nil {
				resolutions[path] = resolution
			}
		}
		if resolution.Matched && resolution.Release.Ref.Name != "" {
			if idx == nil {
				idx = workspace.NewPackageIndex()
				idx.Merge(observed.Packages)
			}
			idx.Add(identifiedPackage(path, resolution.Release.Ref))
		} else {
			unidentified = append(unidentified, path)
		}
		for _, providerErr := range resolution.Errors {
			errs = append(errs, fmt.Errorf("%s: %w", path, providerErr))
		}
	}

	if idx != nil {
		observed.Packages = idx.Packages()
	}
	observed.UnidentifiedPackageFiles = unidentified
	return errs
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
