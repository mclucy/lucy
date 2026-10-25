package operation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/resolve"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
	versionpkg "github.com/mclucy/lucy/version"
)

const (
	defaultUpgradeCombinations = 32
	defaultUpgradeVersions     = 6
	maxUpgradeFailures         = 5
)

// UpgradeOptions selects the manifest roots an upgrade may advance. An empty
// Targets with All false is an error; callers must say what to upgrade.
type UpgradeOptions struct {
	All     bool
	Targets []string
	// Offline disallows network acquisition. Upgrade cannot discover newer
	// versions offline, so this is only useful when it fails early.
	Offline bool
	Trace   *install.Trace
	// MaxCombinations and MaxVersions bound the frontier search. Zero uses
	// the package defaults.
	MaxCombinations int
	MaxVersions     int
}

// UpgradeMetrics scores one frontier point. UpgradedRoots counts manifest
// roots whose selected build differs from the lock. VersionBumps sums the
// number of newer eligible versions skipped for roots whose version changed.
type UpgradeMetrics struct {
	UpgradedRoots int `json:"upgraded_roots"`
	VersionBumps  int `json:"version_bumps"`
}

// UpgradeOption is one resolved plan and its policy metrics.
type UpgradeOption struct {
	Hash    string         `json:"hash"`
	Metrics UpgradeMetrics `json:"metrics"`
	Plan    *install.Plan  `json:"-"`
}

// UpgradeResult carries the bounded Pareto frontier and non-fatal findings.
type UpgradeResult struct {
	BaseManifestFingerprint string
	BaseLockFingerprint     string
	Options                 []UpgradeOption
	Warnings                []error
}

type upgradeRoot struct {
	Environment serverpkg.Environment
	Ecosystem   catalog.Ecosystem
	Reference   string
	Requirement manifest.Requirement
	Provider    catalog.Provider
	Project     string
	Previous    *lockfile.Package
	Versions    []upgradeVersion
	LockedIndex int
	LockedRank  int
}

type upgradeVersion struct {
	Version     versionpkg.Version
	Builds      []upstream.Candidate
	Rank        int
	PublishedAt time.Time
}

// PlanUpgrade resolves candidate frontiers without staging or committing.
func PlanUpgrade(
	ctx context.Context,
	root string,
	opts UpgradeOptions,
) (result *UpgradeResult, resultErr error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root = absolute
	d, err := manifest.Read(root)
	if err != nil {
		return nil, err
	}
	old, err := lockfile.Read(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("lucy-lock.yaml is required; run lucy install first")
		}
		return nil, err
	}
	baseManifest, err := manifest.Fingerprint(d)
	if err != nil {
		return nil, err
	}
	if old.ManifestFingerprint != baseManifest {
		return nil, fmt.Errorf("manifest changed since the lock; run lucy install first")
	}
	if err := upgradePreflight(ctx, root, d, old); err != nil {
		return nil, err
	}
	baseLock, err := lockfile.Fingerprint(old)
	if err != nil {
		return nil, err
	}
	writer, err := workspacewrite.Lock(root)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := workspacewrite.RecoverInstall(root); err != nil {
		return nil, err
	}
	currentManifest, err := manifest.Read(root)
	if err != nil {
		return nil, err
	}
	currentManifestFingerprint, err := manifest.Fingerprint(currentManifest)
	if err != nil {
		return nil, err
	}
	if currentManifestFingerprint != baseManifest {
		return nil, fmt.Errorf("manifest changed during upgrade planning; retry")
	}
	currentLock, err := lockfile.Read(root)
	if err != nil {
		return nil, err
	}
	currentLockFingerprint, err := lockfile.Fingerprint(currentLock)
	if err != nil {
		return nil, err
	}
	if currentLockFingerprint != baseLock {
		return nil, fmt.Errorf("lock changed during upgrade planning; retry")
	}
	roots, err := upgradeRoots(d, old)
	if err != nil {
		return nil, err
	}
	targets, err := upgradeTargets(roots, opts)
	if err != nil {
		return nil, err
	}
	warnings := []error{}
	materialize, fetch := install.Materializer(ctx, root, install.Options{
		Offline: opts.Offline,
		Trace:   opts.Trace,
	})
	cache := resolve.NewAnalysisCache()
	maxVersions := opts.MaxVersions
	if maxVersions <= 0 {
		maxVersions = defaultUpgradeVersions
	}
	active := make([]int, 0, len(roots))
	for i := range roots {
		if !targets[roots[i].Reference] {
			continue
		}
		versions, lockedIndex, lockedRank, err := upgradeVersions(ctx, roots[i], old, maxVersions)
		if err != nil {
			if opts.All {
				warnings = append(warnings, fmt.Errorf(
					"keep %s at %s: %w",
					roots[i].Reference,
					roots[i].Previous.Version,
					err,
				))
				continue
			}
			return nil, err
		}
		roots[i].Versions = versions
		roots[i].LockedIndex = lockedIndex
		roots[i].LockedRank = lockedRank
		if lockedIndex > 0 {
			active = append(active, i)
		}
	}
	if len(active) == 0 {
		option, err := noopUpgradeOption(d, old, baseManifest, baseLock)
		if err != nil {
			return nil, err
		}
		return &UpgradeResult{
			BaseManifestFingerprint: baseManifest,
			BaseLockFingerprint:     baseLock,
			Options:                 []UpgradeOption{option},
			Warnings:                warnings,
		}, nil
	}
	maxCombinations := opts.MaxCombinations
	if maxCombinations <= 0 {
		maxCombinations = defaultUpgradeCombinations
	}
	combos := upgradeCombinations(roots, active, maxCombinations)
	seen := map[string]UpgradeOption{}
	failures := []error{}
	for _, combo := range combos {
		option, err := resolveUpgradeOption(
			ctx,
			root,
			d,
			old,
			baseManifest,
			baseLock,
			roots,
			active,
			combo,
			materialize,
			cache,
			opts.Trace,
		)
		if err != nil {
			log.Debug("upgrade: candidate combination failed", "error", err)
			if len(failures) < maxUpgradeFailures {
				failures = append(failures, err)
			}
			continue
		}
		seen[option.Hash] = option
	}
	if len(seen) == 0 {
		if len(failures) == 0 {
			return nil, fmt.Errorf("no eligible upgrade found")
		}
		return nil, fmt.Errorf("no valid upgrade option: %w", errors.Join(failures...))
	}
	options := make([]UpgradeOption, 0, len(seen))
	for _, option := range seen {
		if option.Plan != nil && option.Plan.Result != nil {
			option.Plan.Result.Fetch = *fetch
		}
		options = append(options, option)
	}
	if len(failures) > 0 {
		warnings = append(warnings, fmt.Errorf(
			"%d candidate combinations failed; showing the resolvable frontier",
			len(failures),
		))
	}
	return &UpgradeResult{
		BaseManifestFingerprint: baseManifest,
		BaseLockFingerprint:     baseLock,
		Options:                 paretoFrontier(options),
		Warnings:                warnings,
	}, nil
}

// ApplyUpgrade applies one selected option through the install transaction.
func ApplyUpgrade(
	ctx context.Context,
	root string,
	option UpgradeOption,
	policy string,
	opts install.Options,
) (*install.Result, error) {
	if option.Plan == nil {
		return nil, fmt.Errorf("upgrade option has no plan")
	}
	option.Plan.Result.Policy = policy
	option.Plan.Result.PlanHash = option.Hash
	return install.ApplyPlan(ctx, root, option.Plan, opts)
}

// SelectUpgradeOption picks a frontier point for a policy value. A policy that
// is not more/later/options is treated as a plan hash.
func SelectUpgradeOption(result *UpgradeResult, policy string) (*UpgradeOption, error) {
	if result == nil || len(result.Options) == 0 {
		return nil, fmt.Errorf("no upgrade options")
	}
	switch policy {
	case "", "more", "later":
	case "options":
		return nil, fmt.Errorf("policy options has no single selection")
	default:
		for i := range result.Options {
			if result.Options[i].Hash == policy {
				return &result.Options[i], nil
			}
		}
		return nil, fmt.Errorf("plan hash %q was not found; rerun with --dry-run or --policy options", policy)
	}
	best := &result.Options[0]
	for i := range result.Options[1:] {
		candidate := &result.Options[i+1]
		switch policy {
		case "later":
			if candidate.Metrics.VersionBumps > best.Metrics.VersionBumps ||
				(candidate.Metrics.VersionBumps == best.Metrics.VersionBumps &&
					candidate.Metrics.UpgradedRoots > best.Metrics.UpgradedRoots) {
				best = candidate
			}
		default:
			if candidate.Metrics.UpgradedRoots > best.Metrics.UpgradedRoots ||
				(candidate.Metrics.UpgradedRoots == best.Metrics.UpgradedRoots &&
					candidate.Metrics.VersionBumps > best.Metrics.VersionBumps) {
				best = candidate
			}
		}
	}
	return best, nil
}

// ListUpgradeRoots returns the enabled manifest roots a user can select.
func ListUpgradeRoots(root string) ([]string, error) {
	d, err := manifest.Read(root)
	if err != nil {
		return nil, err
	}
	var refs []string
	for _, ecosystem := range sortedEcosystems(d.Server.Packages) {
		for ref, requirement := range d.Server.Packages[ecosystem] {
			if requirement.IsEnabled() {
				refs = append(refs, ref)
			}
		}
	}
	if d.MCDR != nil {
		for ref, requirement := range d.MCDR.Packages {
			if requirement.IsEnabled() {
				refs = append(refs, ref)
			}
		}
	}
	slices.Sort(refs)
	return refs, nil
}

func upgradePreflight(
	ctx context.Context,
	root string,
	d *manifest.Document,
	old *lockfile.Document,
) error {
	if d.Server.Minecraft != old.Server.Minecraft {
		return fmt.Errorf("Minecraft %s differs from locked %s; run lucy migrate", d.Server.Minecraft, old.Server.Minecraft)
	}
	if d.Server.Core.Product != old.Server.Product {
		return fmt.Errorf("server product %s differs from locked %s; run lucy migrate", d.Server.Core.Product, old.Server.Product)
	}
	if !upgradeFloating(d.Server.Core.ProductVersion) && d.Server.Core.ProductVersion != old.Server.CoreVersion {
		return fmt.Errorf("server core %s differs from locked %s; run lucy migrate", d.Server.Core.ProductVersion, old.Server.CoreVersion)
	}
	switch {
	case d.MCDR == nil && old.MCDR != nil:
		return fmt.Errorf("manifest removed MCDR; run lucy install or lucy migrate")
	case d.MCDR != nil && old.MCDR == nil:
		return fmt.Errorf("manifest added MCDR; run lucy install or lucy migrate")
	case d.MCDR != nil && old.MCDR != nil &&
		!upgradeFloating(d.MCDR.Version) && d.MCDR.Version != old.MCDR.Version:
		return fmt.Errorf("MCDR %s differs from locked %s; run lucy migrate", d.MCDR.Version, old.MCDR.Version)
	}
	check, err := install.CheckJava(ctx, root, old)
	if err != nil {
		return err
	}
	if !check.Satisfied() {
		if len(check.Problems) > 0 {
			return fmt.Errorf("Java runtime does not satisfy the lock: %s", strings.Join(check.Problems, "; "))
		}
		return fmt.Errorf("Java %d is required; run lucy install --java", check.Target)
	}
	return nil
}

func upgradeFloating(v versionpkg.Version) bool {
	switch v {
	case "", versionpkg.Any, versionpkg.Beta, versionpkg.Stable, "latest":
		return true
	default:
		return false
	}
}

func upgradeRoots(d *manifest.Document, old *lockfile.Document) ([]upgradeRoot, error) {
	var roots []upgradeRoot
	for _, ecosystem := range sortedEcosystems(d.Server.Packages) {
		for ref, requirement := range d.Server.Packages[ecosystem] {
			if !requirement.IsEnabled() {
				continue
			}
			root, err := newUpgradeRoot(serverpkg.EnvironmentServer, ecosystem, ref, requirement, old)
			if err != nil {
				return nil, err
			}
			roots = append(roots, root)
		}
	}
	if d.MCDR != nil {
		for ref, requirement := range d.MCDR.Packages {
			if !requirement.IsEnabled() {
				continue
			}
			root, err := newUpgradeRoot(serverpkg.EnvironmentMCDR, catalog.EcosystemMCDR, ref, requirement, old)
			if err != nil {
				return nil, err
			}
			roots = append(roots, root)
		}
	}
	slices.SortFunc(roots, func(a, b upgradeRoot) int {
		if n := strings.Compare(string(a.Environment), string(b.Environment)); n != 0 {
			return n
		}
		if n := strings.Compare(string(a.Ecosystem), string(b.Ecosystem)); n != 0 {
			return n
		}
		return strings.Compare(a.Reference, b.Reference)
	})
	return roots, nil
}

func newUpgradeRoot(
	environment serverpkg.Environment,
	ecosystem catalog.Ecosystem,
	ref string,
	requirement manifest.Requirement,
	old *lockfile.Document,
) (upgradeRoot, error) {
	provider, project, err := manifest.ParseReference(ref)
	if err != nil {
		return upgradeRoot{}, err
	}
	previous := previousRootPackage(old, environment, ecosystem, ref, provider, project)
	if previous == nil {
		return upgradeRoot{}, fmt.Errorf("lock has no package for manifest root %s; run lucy install first", ref)
	}
	return upgradeRoot{
		Environment: environment,
		Ecosystem:   ecosystem,
		Reference:   ref,
		Requirement: requirement,
		Provider:    provider,
		Project:     project,
		Previous:    previous,
	}, nil
}

func previousRootPackage(
	old *lockfile.Document,
	environment serverpkg.Environment,
	ecosystem catalog.Ecosystem,
	ref string,
	provider catalog.Provider,
	project string,
) *lockfile.Package {
	for i := range old.Packages {
		p := &old.Packages[i]
		if p.Environment != environment || p.Ecosystem != ecosystem {
			continue
		}
		for _, binding := range p.Requirements {
			if binding.Reference == ref {
				return p
			}
		}
	}
	for i := range old.Packages {
		p := &old.Packages[i]
		if p.Environment == environment && p.Ecosystem == ecosystem &&
			p.Provider == provider && p.ProjectID == project {
			return p
		}
	}
	return nil
}

func upgradeTargets(roots []upgradeRoot, opts UpgradeOptions) (map[string]bool, error) {
	switch {
	case opts.All && len(opts.Targets) > 0:
		return nil, fmt.Errorf("--all cannot be combined with package arguments")
	case opts.All:
		targets := make(map[string]bool, len(roots))
		for _, root := range roots {
			targets[root.Reference] = true
		}
		return targets, nil
	case len(opts.Targets) == 0:
		return nil, fmt.Errorf("specify packages or --all")
	}
	targets := map[string]bool{}
	for _, raw := range opts.Targets {
		request, err := input.Parse(raw)
		if err != nil {
			return nil, err
		}
		if request.Version != "" && request.Version != versionpkg.Any {
			return nil, fmt.Errorf("upgrade targets name packages, not versions: %s", raw)
		}
		var matched []string
		for _, root := range roots {
			if request.Provider != catalog.ProviderUnspecified && request.Provider != root.Provider {
				continue
			}
			if !strings.EqualFold(string(request.Name), root.Project) {
				continue
			}
			matched = append(matched, root.Reference)
		}
		switch len(matched) {
		case 0:
			return nil, fmt.Errorf("package %s is not an enabled manifest root", raw)
		case 1:
			targets[matched[0]] = true
		default:
			return nil, fmt.Errorf("package %s is ambiguous: %s", raw, strings.Join(matched, ", "))
		}
	}
	return targets, nil
}

func upgradeVersions(
	ctx context.Context,
	root upgradeRoot,
	old *lockfile.Document,
	maxVersions int,
) ([]upgradeVersion, int, int, error) {
	registry, ok := root.Provider.Registry()
	if !ok {
		return nil, 0, 0, fmt.Errorf("provider %s has no package catalogue", root.Provider)
	}
	list, err := routing.Candidates(
		ctx,
		registry,
		root.Project,
		string(old.Server.Minecraft),
		root.Ecosystem,
	)
	if err != nil {
		return nil, 0, 0, err
	}
	selector := root.Requirement.Selector
	hasRelease := false
	for _, candidate := range list {
		if candidate.Channel == upstream.ReleaseChannelRelease {
			hasRelease = true
			break
		}
	}
	eligible := make([]upstream.Candidate, 0, len(list))
	for _, candidate := range list {
		locked := candidate.ReleaseID == root.Previous.ReleaseID &&
			candidate.FileID == root.Previous.FileID
		if !locked && selector == versionpkg.Stable && candidate.Channel != upstream.ReleaseChannelRelease {
			continue
		}
		if !locked &&
			(selector == versionpkg.Any || selector == "" || !selector.CanInfer()) &&
			hasRelease && candidate.Channel != upstream.ReleaseChannelRelease {
			continue
		}
		if root.Requirement.FilePattern != "" {
			match, err := filepath.Match(root.Requirement.FilePattern, candidate.Filename)
			if err != nil {
				return nil, 0, 0, err
			}
			if !match {
				continue
			}
		} else if !candidate.PrimaryFile {
			continue
		}
		eligible = append(eligible, candidate)
	}
	if len(eligible) == 0 {
		return nil, 0, 0, fmt.Errorf("no eligible builds for Minecraft %s", old.Server.Minecraft)
	}
	byVersion := map[versionpkg.Version][]upstream.Candidate{}
	for _, candidate := range eligible {
		byVersion[candidate.Version] = append(byVersion[candidate.Version], candidate)
	}
	versions := make([]upgradeVersion, 0, len(byVersion))
	for version, builds := range byVersion {
		slices.SortStableFunc(builds, func(a, b upstream.Candidate) int {
			if n := b.PublishedAt.Compare(a.PublishedAt); n != 0 {
				return n
			}
			if n := strings.Compare(a.ReleaseID, b.ReleaseID); n != 0 {
				return n
			}
			return strings.Compare(a.FileID, b.FileID)
		})
		versions = append(versions, upgradeVersion{
			Version:     version,
			Builds:      builds,
			PublishedAt: builds[0].PublishedAt,
		})
	}
	slices.SortFunc(versions, func(a, b upgradeVersion) int {
		if n := b.PublishedAt.Compare(a.PublishedAt); n != 0 {
			return n
		}
		return strings.Compare(string(b.Version), string(a.Version))
	})
	lockedRank := -1
	for i := range versions {
		versions[i].Rank = i
		if versions[i].Version == root.Previous.Version {
			lockedRank = i
			continue
		}
		for _, build := range versions[i].Builds {
			if build.ReleaseID == root.Previous.ReleaseID && build.FileID == root.Previous.FileID {
				lockedRank = i
			}
		}
	}
	if lockedRank < 0 {
		return nil, 0, 0, fmt.Errorf("locked version %s is no longer listed", root.Previous.Version)
	}
	trimmed := versions
	lockedIndex := lockedRank
	if len(versions) > maxVersions {
		if lockedRank < maxVersions {
			trimmed = versions[:maxVersions]
			lockedIndex = lockedRank
		} else {
			trimmed = append([]upgradeVersion{}, versions[:maxVersions-1]...)
			trimmed = append(trimmed, versions[lockedRank])
			lockedIndex = len(trimmed) - 1
		}
	}
	return trimmed, lockedIndex, lockedRank, nil
}

func upgradeCombinations(roots []upgradeRoot, active []int, limit int) [][]int {
	locked := make([]int, len(active))
	for i, rootIndex := range active {
		locked[i] = roots[rootIndex].LockedIndex
	}
	seeds := []int{}
	for range active {
		seeds = append(seeds, 0)
	}
	queue := [][]int{seeds, locked}
	seen := map[string]bool{}
	var combos [][]int
	for len(queue) > 0 && len(combos) < limit {
		combo := queue[0]
		queue = queue[1:]
		key := fmt.Sprint(combo)
		if seen[key] {
			continue
		}
		seen[key] = true
		combos = append(combos, combo)
		for i, rootIndex := range active {
			if combo[i]+1 > roots[rootIndex].LockedIndex {
				continue
			}
			next := slices.Clone(combo)
			next[i]++
			queue = append(queue, next)
		}
	}
	slices.SortFunc(combos, func(a, b []int) int {
		sumA, sumB := 0, 0
		for i := range a {
			sumA += a[i]
			sumB += b[i]
		}
		if sumA != sumB {
			return sumA - sumB
		}
		for i := range a {
			if a[i] != b[i] {
				return a[i] - b[i]
			}
		}
		return 0
	})
	return combos
}

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
	publishManifest := len(manifestChanges) > 0
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

func pinCoreAndMCDR(d *manifest.Document, old *lockfile.Document) {
	d.Server.Minecraft = old.Server.Minecraft
	d.Server.Core.Product = old.Server.Product
	d.Server.Core.ProductVersion = old.Server.CoreVersion
	if d.MCDR != nil && old.MCDR != nil {
		d.MCDR.Version = old.MCDR.Version
	}
}

func upgradeTarget(old *lockfile.Document) resolve.Target {
	mcdrVersion := ""
	if old.MCDR != nil {
		mcdrVersion = string(old.MCDR.Version)
	}
	return resolve.Target{
		Product:      old.Server.Product,
		Bootstrapper: old.Server.Bootstrapper,
		Minecraft:    old.Server.Minecraft,
		CoreVersion:  old.Server.CoreVersion,
		MCDRVersion:  mcdrVersion,
	}
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

func rootRequirements(d *manifest.Document) map[string]manifest.Requirement {
	requirements := map[string]manifest.Requirement{}
	for _, ecosystem := range sortedEcosystems(d.Server.Packages) {
		for ref, requirement := range d.Server.Packages[ecosystem] {
			requirements[ref] = requirement
		}
	}
	if d.MCDR != nil {
		for ref, requirement := range d.MCDR.Packages {
			requirements[ref] = requirement
		}
	}
	return requirements
}

func setRootSelector(d *manifest.Document, root upgradeRoot, selector versionpkg.Version) error {
	if root.Environment == serverpkg.EnvironmentMCDR {
		if d.MCDR == nil {
			return fmt.Errorf("manifest has no MCDR runtime")
		}
		requirement, ok := d.MCDR.Packages[root.Reference]
		if !ok {
			return fmt.Errorf("manifest root %s is missing", root.Reference)
		}
		requirement.Selector = selector
		d.MCDR.Packages[root.Reference] = requirement
		return nil
	}
	packages, ok := d.Server.Packages[root.Ecosystem]
	if !ok {
		return fmt.Errorf("manifest ecosystem %s is missing", root.Ecosystem)
	}
	requirement, ok := packages[root.Reference]
	if !ok {
		return fmt.Errorf("manifest root %s is missing", root.Reference)
	}
	requirement.Selector = selector
	packages[root.Reference] = requirement
	return nil
}

func cloneManifest(d *manifest.Document) (*manifest.Document, error) {
	data, err := manifest.CanonicalBytes(d)
	if err != nil {
		return nil, err
	}
	return manifest.Decode(bytes.NewReader(data))
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

func sortedEcosystems(packages map[catalog.Ecosystem]map[string]manifest.Requirement) []catalog.Ecosystem {
	ecosystems := make([]catalog.Ecosystem, 0, len(packages))
	for ecosystem := range packages {
		ecosystems = append(ecosystems, ecosystem)
	}
	slices.Sort(ecosystems)
	return ecosystems
}
