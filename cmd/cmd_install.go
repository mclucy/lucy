package cmd

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/state"
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

	plan, err := buildInstallSyncPlan(stateSvc.Manifest(), stateSvc.Lock())
	if err != nil {
		return err
	}
	if len(plan.Requested) == 0 {
		return nil
	}

	options := install.DefaultOptions()

	result, err := install.InstallMany(cmd.Context(), plan.Requested, options)
	if err != nil {
		var conflictErr *resolve.ConstraintConflictError
		if errors.As(err, &conflictErr) {
			return formatConstraintConflict(conflictErr)
		}
		return err
	}

	lock := buildUpdatedLock(
		workDir,
		stateSvc.Manifest(),
		stateSvc.Lock(),
		result,
	)
	return stateSvc.Save(cmd.Context(), nil, lock)
}

func buildInstallSyncPlan(
	manifest *state.Manifest,
	lock *state.Lock,
) (installSyncPlan, error) {
	if manifest == nil {
		return installSyncPlan{}, fmt.Errorf("manifest is required for install")
	}

	exact, ok, err := exactSyncPackageIDs(manifest, lock)
	if err != nil {
		return installSyncPlan{}, err
	}
	if ok {
		return installSyncPlan{
			Requested: exact, UsesExactLock: true, Stable: true,
		}, nil
	}

	required, err := manifestRequiredPackageIDs(manifest)
	if err != nil {
		return installSyncPlan{}, err
	}
	return installSyncPlan{
		Requested: required, UsesExactLock: false, Stable: false,
	}, nil
}

func exactSyncPackageIDs(
	manifest *state.Manifest,
	lock *state.Lock,
) ([]types.PackageRequest, bool, error) {
	if manifest == nil || lock == nil || len(lock.Packages) == 0 {
		return nil, false, nil
	}
	if manifestFingerprint(manifest, "") != lock.ManifestFingerprint {
		return nil, false, nil
	}

	if len(lock.Packages) == 0 {
		return nil, false, nil
	}

	diff := state.DiffDesiredResolved(managedManifest(manifest), lock)
	if len(diff.InManifestNotLock) > 0 || len(diff.InLockNotManifest) > 0 {
		return nil, false, nil
	}

	requested := make([]types.PackageRequest, 0, len(lock.Packages))
	for _, pkg := range lock.Packages {
		ref, version, err := input.Parse(pkg.ID + "@" + pkg.Version)
		if err != nil {
			return nil, false, fmt.Errorf(
				"parse locked package %s: %w",
				pkg.ID,
				err,
			)
		}
		requested = append(
			requested, types.PackageRequest{
				FullPackageRef: types.FullPackageRef{
					PackageRef: ref.PackageRef,
					Version:    version,
					Scope:      types.ParseSource(pkg.Source),
				},
			},
		)
	}

	sort.Slice(
		requested, func(i, j int) bool {
			left := requested[i].PackageRef.StringBase()
			right := requested[j].PackageRef.StringBase()
			if left != right {
				return left < right
			}
			return requested[i].Version.String() < requested[j].Version.String()
		},
	)

	return requested, true, nil
}

func manifestRequiredPackageIDs(manifest *state.Manifest) (
	[]types.PackageRequest,
	error,
) {
	requested := make([]types.PackageRequest, 0, len(manifest.Packages))
	for _, pkg := range manifest.Packages {
		if pkg.Role != state.RoleRequired {
			continue
		}
		ref, version, err := input.Parse(pkg.ID + "@" + pkg.Version)
		if err != nil {
			return nil, fmt.Errorf("parse manifest package %s: %w", pkg.ID, err)
		}
		requested = append(
			requested, types.PackageRequest{
				FullPackageRef: types.FullPackageRef{
					PackageRef: ref.PackageRef,
					Version:    version,
					Scope:      types.ParseSource(pkg.Source),
				},
			},
		)
	}

	sort.Slice(
		requested, func(i, j int) bool {
			return requested[i].PackageRef.StringBase() < requested[j].PackageRef.StringBase()
		},
	)
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
