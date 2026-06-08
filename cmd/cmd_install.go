package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/types"
	"github.com/spf13/cobra"
)

type installSyncPlan struct {
	Requested     []types.PackageRequest
	UsesExactLock bool
	Stable        bool
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Converge Lucy-managed runtime state from the lockfile",
	Args:  cobra.NoArgs,
	RunE:  runWithErrorLogging(actionInstall),
}

func init() {
	addNoStyleFlag(installCmd)
	rootCmd.AddCommand(installCmd)
}

func actionInstall(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}

	hasLucyState, err := lucyStateDirExists(workDir)
	if err != nil {
		return err
	}
	if !hasLucyState {
		return fmt.Errorf("lucy state is not initialized")
	}

	stateSvc := state.NewProjectStateService(workDir)
	if err := stateSvc.Load(cmd.Context()); err != nil {
		return fmt.Errorf("load lucy state: %w", err)
	}
	if stateSvc.Manifest() == nil {
		return fmt.Errorf("manifest is required for install")
	}

	plan, err := buildInstallSyncPlan(stateSvc.Manifest(), stateSvc.Lock(), stateSvc.Config())
	if err != nil {
		return err
	}
	if len(plan.Requested) == 0 {
		return nil
	}

	options := install.DefaultOptions()
	if cfg := stateSvc.Config(); cfg != nil {
		options.WithOptional = cfg.Optional.IncludeOptional
	}

	result, err := install.InstallMany(plan.Requested, options)
	if err != nil {
		return err
	}

	lock := buildUpdatedLock(workDir, stateSvc.Manifest(), stateSvc.Lock(), result)
	lock = filteredManagedLock(stateSvc.Config(), stateSvc.Manifest(), lock)
	return state.WriteLock(workDir, lock)
}

func buildInstallSyncPlan(manifest *state.Manifest, lock *state.Lock, config *state.Config) (installSyncPlan, error) {
	if manifest == nil {
		return installSyncPlan{}, fmt.Errorf("manifest is required for install")
	}

	exact, ok, err := exactSyncPackageIDs(manifest, lock, config)
	if err != nil {
		return installSyncPlan{}, err
	}
	if ok {
		return installSyncPlan{Requested: exact, UsesExactLock: true, Stable: true}, nil
	}

	required, err := manifestRequiredPackageIDs(manifest)
	if err != nil {
		return installSyncPlan{}, err
	}
	return installSyncPlan{Requested: required, UsesExactLock: false, Stable: false}, nil
}

func exactSyncPackageIDs(manifest *state.Manifest, lock *state.Lock, config *state.Config) ([]types.PackageRequest, bool, error) {
	if manifest == nil || lock == nil || len(lock.Packages) == 0 {
		return nil, false, nil
	}
	if manifestFingerprint(manifest, "") != lock.ManifestFingerprint {
		return nil, false, nil
	}

	filteredLock := filteredManagedLock(config, manifest, lock)
	if len(filteredLock.Packages) == 0 {
		return nil, false, nil
	}

	diff := state.DiffDesiredResolved(managedManifest(manifest), filteredLock)
	if len(diff.InManifestNotLock) > 0 || len(diff.InLockNotManifest) > 0 {
		return nil, false, nil
	}

	requested := make([]types.PackageRequest, 0, len(filteredLock.Packages))
	for _, pkg := range filteredLock.Packages {
		id, err := syntax.Parse(pkg.ID + "@" + pkg.Version)
		if err != nil {
			return nil, false, fmt.Errorf("parse locked package %s: %w", pkg.ID, err)
		}
		// TODO(package-ref-migration): remove wrapping once lock parsing returns PackageRequest.
		requested = append(requested, types.PackageRequest{
			Ref:     types.PackageRef{Platform: id.Platform, Name: id.Name},
			Version: id.Version,
		})
	}

	sort.Slice(requested, func(i, j int) bool {
		left := string(requested[i].Ref.Platform) + "/" + string(requested[i].Ref.Name)
		right := string(requested[j].Ref.Platform) + "/" + string(requested[j].Ref.Name)
		if left != right {
			return left < right
		}
		return requested[i].Version.String() < requested[j].Version.String()
	})

	return requested, true, nil
}

func manifestRequiredPackageIDs(manifest *state.Manifest) ([]types.PackageRequest, error) {
	requested := make([]types.PackageRequest, 0, len(manifest.Packages))
	for _, pkg := range manifest.Packages {
		if pkg.Role != state.RoleRequired {
			continue
		}
		id, err := syntax.Parse(pkg.ID + "@" + pkg.Version)
		if err != nil {
			return nil, fmt.Errorf("parse manifest package %s: %w", pkg.ID, err)
		}
		// TODO(package-ref-migration): remove wrapping once manifest parsing returns PackageRequest.
		requested = append(requested, types.PackageRequest{
			Ref:     types.PackageRef{Platform: id.Platform, Name: id.Name},
			Version: id.Version,
		})
	}

	sort.Slice(requested, func(i, j int) bool {
		left := string(requested[i].Ref.Platform) + "/" + string(requested[i].Ref.Name)
		right := string(requested[j].Ref.Platform) + "/" + string(requested[j].Ref.Name)
		return left < right
	})
	return requested, nil
}

func managedManifest(manifest *state.Manifest) *state.Manifest {
	if manifest == nil {
		return nil
	}

	cloned := *manifest
	cloned.Packages = make([]state.ManifestPackage, 0, len(manifest.Packages))
	for _, pkg := range manifest.Packages {
		if pkg.Role == state.RoleIgnored {
			continue
		}
		cloned.Packages = append(cloned.Packages, pkg)
	}
	return &cloned
}

func filteredManagedLock(config *state.Config, manifest *state.Manifest, lock *state.Lock) *state.Lock {
	if lock == nil {
		return nil
	}

	filtered := *lock
	filtered.Packages = make([]state.LockedPackage, 0, len(lock.Packages))
	filtered.Bundles = append([]state.LockedBundle(nil), lock.Bundles...)

	ignored := make(map[string]struct{})
	scope := state.NewManagedScope(nil, nil)
	if config != nil {
		scope = state.NewManagedScope(config.Scope.ManagedRoots, config.Scope.UnmanagedPaths)
	}
	if manifest != nil {
		for _, pkg := range manifest.Packages {
			if pkg.Role == state.RoleIgnored {
				ignored[pkg.ID] = struct{}{}
			}
		}
	}

	for _, pkg := range lock.Packages {
		if _, skip := ignored[pkg.ID]; skip {
			continue
		}
		if !state.IsManaged(scope, pkg.InstallPath) {
			continue
		}
		filtered.Packages = append(filtered.Packages, pkg)
	}
	filtered.Packages = state.CanonicalLockedPackages(filtered.Packages)
	return &filtered
}
