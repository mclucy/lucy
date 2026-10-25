package operation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/resolve"
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
