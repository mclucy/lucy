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
	Requested     []install.PackageRequest
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

	result, err := install.InstallMany(plan.Requested, options)
	if err != nil {
		return err
	}

	lock := buildUpdatedLock(
		workDir,
		stateSvc.Manifest(),
		stateSvc.Lock(),
		result,
	)
	return state.WriteLock(workDir, lock)
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
) ([]install.PackageRequest, bool, error) {
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

	requested := make([]install.PackageRequest, 0, len(lock.Packages))
	for _, pkg := range lock.Packages {
		id, err := syntax.Parse(pkg.ID + "@" + pkg.Version)
		if err != nil {
			return nil, false, fmt.Errorf(
				"parse locked package %s: %w",
				pkg.ID,
				err,
			)
		}
		// TODO(package-ref-migration): remove wrapping once lock parsing returns PackageRequest.
		requested = append(
			requested, install.PackageRequest{
				Ref:     types.PackageRef{Platform: id.Platform, Name: id.Name},
				Version: id.Version,
			},
		)
	}

	sort.Slice(
		requested, func(i, j int) bool {
			left := string(requested[i].Ref.Platform) + "/" + string(requested[i].Ref.Name)
			right := string(requested[j].Ref.Platform) + "/" + string(requested[j].Ref.Name)
			if left != right {
				return left < right
			}
			return requested[i].Version.String() < requested[j].Version.String()
		},
	)

	return requested, true, nil
}

func manifestRequiredPackageIDs(manifest *state.Manifest) (
	[]install.PackageRequest,
	error,
) {
	requested := make([]install.PackageRequest, 0, len(manifest.Packages))
	for _, pkg := range manifest.Packages {
		if pkg.Role != state.RoleRequired {
			continue
		}
		id, err := syntax.Parse(pkg.ID + "@" + pkg.Version)
		if err != nil {
			return nil, fmt.Errorf("parse manifest package %s: %w", pkg.ID, err)
		}
		// TODO(package-ref-migration): remove wrapping once manifest parsing returns PackageRequest.
		requested = append(
			requested, install.PackageRequest{
				Ref:     types.PackageRef{Platform: id.Platform, Name: id.Name},
				Version: id.Version,
			},
		)
	}

	sort.Slice(
		requested, func(i, j int) bool {
			left := string(requested[i].Ref.Platform) + "/" + string(requested[i].Ref.Name)
			right := string(requested[j].Ref.Platform) + "/" + string(requested[j].Ref.Name)
			return left < right
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
