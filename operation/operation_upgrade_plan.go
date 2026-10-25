package operation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

func resolveUpgradeOption(
	ctx context.Context,
	root string,
	d *manifest.Document,
	old *lockfile.Document,
	baseManifest string,
	baseLock string,
	roots []upgradeRoot,
	active []int,
	combo []int,
	materialize func(*catalog.Artifact) (artifact.MaterializedFile, error),
	cache *resolve.AnalysisCache,
	trace *install.Trace,
) (UpgradeOption, error) {
	resolutionManifest, err := cloneManifest(d)
	if err != nil {
		return UpgradeOption{}, err
	}
	pinCoreAndMCDR(resolutionManifest, old)
	choices := map[string]versionpkg.Version{}
	for i, rootIndex := range active {
		root := roots[rootIndex]
		choices[root.Reference] = root.Versions[combo[i]].Version
	}
	for _, root := range roots {
		selector := root.Requirement.Selector
		if choice, ok := choices[root.Reference]; ok {
			selector = choice
		} else if root.Previous != nil {
			selector = upgradeBuildSelector(upstream.Candidate{Artifact: root.Previous.Artifact})
		}
		if err := setRootSelector(resolutionManifest, root, selector); err != nil {
			return UpgradeOption{}, err
		}
	}
	if err := manifest.Normalize(resolutionManifest); err != nil {
		return UpgradeOption{}, err
	}
	packages, _, err := resolve.Packages(resolve.PackagesRequest{
		Context:            ctx,
		Manifest:           resolutionManifest,
		Previous:           old.Packages,
		Target:             upgradeTarget(old),
		Materialize:        materialize,
		ExpandDependencies: true,
		Cache:              cache,
		Trace:              upgradeResolveTrace(trace),
	})
	if err != nil {
		return UpgradeOption{}, err
	}
	finalManifest, err := cloneManifest(d)
	if err != nil {
		return UpgradeOption{}, err
	}
	var manifestChanges []install.ManifestChange
	for _, rootIndex := range active {
		root := roots[rootIndex]
		selected := selectedRootPackage(packages, root)
		if selected == nil {
			return UpgradeOption{}, fmt.Errorf("resolution did not select %s", root.Reference)
		}
		if root.Requirement.Selector.CanInfer() || root.Requirement.Selector == "" {
			continue
		}
		if selected.Version == root.Requirement.Selector {
			continue
		}
		if err := setRootSelector(finalManifest, root, selected.Version); err != nil {
			return UpgradeOption{}, err
		}
		manifestChanges = append(manifestChanges, install.ManifestChange{
			Reference:        root.Reference,
			PreviousSelector: root.Requirement.Selector,
			Selector:         selected.Version,
		})
	}
	if err := manifest.Normalize(finalManifest); err != nil {
		return UpgradeOption{}, err
	}
	requirements := rootRequirements(finalManifest)
	for i := range packages {
		for j := range packages[i].Requirements {
			if requirement, ok := requirements[packages[i].Requirements[j].Reference]; ok {
				packages[i].Requirements[j].Selector = requirement.Selector
				packages[i].Requirements[j].FilePattern = requirement.FilePattern
				packages[i].Requirements[j].Enabled = requirement.IsEnabled()
			}
		}
	}
	finalFingerprint, err := manifest.Fingerprint(finalManifest)
	if err != nil {
		return UpgradeOption{}, err
	}
	resolved := &lockfile.Document{
		SchemaVersion:       lockfile.SchemaVersion,
		ManifestFingerprint: finalFingerprint,
		Server:              old.Server,
		MCDR:                old.MCDR,
		Packages:            packages,
		Files:               slices.Clone(finalManifest.Files),
	}
	if err := resolved.Validate(); err != nil {
		return UpgradeOption{}, err
	}
	if fingerprint, err := lockfile.Fingerprint(resolved); err == nil && fingerprint == baseLock {
		resolved = old
	}
	metrics := UpgradeMetrics{}
	for _, rootIndex := range active {
		root := roots[rootIndex]
		selected := selectedRootPackage(resolved.Packages, root)
		if selected == nil {
			continue
		}
		changed := selected.Version != root.Previous.Version ||
			selected.ReleaseID != root.Previous.ReleaseID ||
			selected.FileID != root.Previous.FileID
		if changed {
			metrics.UpgradedRoots++
		}
		for _, version := range root.Versions {
			if version.Version == selected.Version {
				if version.Rank < root.LockedRank {
					metrics.VersionBumps += root.LockedRank - version.Rank
				}
				break
			}
		}
	}
	publishManifest := finalFingerprint != baseManifest
	plan, err := install.PlanFromResolution(finalManifest, old, resolved, baseManifest, publishManifest)
	if err != nil {
		return UpgradeOption{}, err
	}
	plan.Result.ManifestChanges = manifestChanges
	hash := upgradePlanHash(baseManifest, baseLock, finalFingerprint, resolved.Packages)
	plan.Result.PlanHash = hash
	return UpgradeOption{
		Hash:    hash,
		Metrics: metrics,
		Plan:    plan,
	}, nil
}

func upgradeBuildSelector(candidate upstream.Candidate) versionpkg.Version {
	if candidate.ReleaseID != "" {
		return versionpkg.Version(candidate.ReleaseID)
	}
	return candidate.Version
}

func upgradeResolveTrace(trace *install.Trace) resolve.Trace {
	if trace == nil {
		return resolve.Trace{}
	}
	return trace.Resolve
}

func selectedRootPackage(packages []lockfile.Package, root upgradeRoot) *lockfile.Package {
	for i := range packages {
		p := &packages[i]
		if p.Environment != root.Environment || p.Ecosystem != root.Ecosystem {
			continue
		}
		for _, binding := range p.Requirements {
			if binding.Reference == root.Reference {
				return p
			}
		}
	}
	return nil
}

func noopUpgradeOption(
	d *manifest.Document,
	old *lockfile.Document,
	baseManifest string,
	baseLock string,
) (UpgradeOption, error) {
	plan, err := install.PlanFromResolution(d, old, old, baseManifest, false)
	if err != nil {
		return UpgradeOption{}, err
	}
	hash := upgradePlanHash(baseManifest, baseLock, old.ManifestFingerprint, old.Packages)
	plan.Result.PlanHash = hash
	return UpgradeOption{
		Hash: hash,
		Plan: plan,
	}, nil
}

func upgradePlanHash(
	baseManifest string,
	baseLock string,
	finalManifest string,
	packages []lockfile.Package,
) string {
	lines := []string{
		"v1",
		"base-manifest:" + baseManifest,
		"base-lock:" + baseLock,
		"final-manifest:" + finalManifest,
	}
	sorted := slices.Clone(packages)
	slices.SortFunc(sorted, func(a, b lockfile.Package) int {
		if n := strings.Compare(string(a.Environment), string(b.Environment)); n != 0 {
			return n
		}
		if n := strings.Compare(string(a.Ecosystem), string(b.Ecosystem)); n != 0 {
			return n
		}
		if n := strings.Compare(string(a.Provider), string(b.Provider)); n != 0 {
			return n
		}
		return strings.Compare(a.ProjectID, b.ProjectID)
	})
	for _, p := range sorted {
		lines = append(lines, strings.Join([]string{
			"pkg",
			string(p.Environment),
			string(p.Ecosystem),
			string(p.Provider),
			p.ProjectID,
			string(p.Version),
			p.ReleaseID,
			p.FileID,
		}, "|"))
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func paretoFrontier(options []UpgradeOption) []UpgradeOption {
	slices.SortFunc(options, func(a, b UpgradeOption) int {
		if a.Metrics.UpgradedRoots != b.Metrics.UpgradedRoots {
			return b.Metrics.UpgradedRoots - a.Metrics.UpgradedRoots
		}
		if a.Metrics.VersionBumps != b.Metrics.VersionBumps {
			return b.Metrics.VersionBumps - a.Metrics.VersionBumps
		}
		return strings.Compare(a.Hash, b.Hash)
	})
	frontier := make([]UpgradeOption, 0, len(options))
	maxBumps := -1
	for _, option := range options {
		if option.Metrics.VersionBumps <= maxBumps {
			continue
		}
		frontier = append(frontier, option)
		maxBumps = option.Metrics.VersionBumps
	}
	return frontier
}
