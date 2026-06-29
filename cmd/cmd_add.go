package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
	"github.com/spf13/cobra"
)

const (
	flagForceName        = "force"
	flagWithOptionalName = "with-optional"
	flagNoOptionalName   = "no-optional"
)

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add packages under explicit operator control",
	Args:  cobra.MinimumNArgs(1),
	ValidArgsFunction: func(
		cmd *cobra.Command,
		args []string,
		toComplete string,
	) ([]string, cobra.ShellCompDirective) {
		return CompletePackageIDSuggestions(
			context.Background(),
			"add",
			toComplete,
		)
	},
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if err := validateSourceFlag(cmd); err != nil {
			return err
		}
		withOptional, _ := cmd.Flags().GetBool(flagWithOptionalName)
		noOptional, _ := cmd.Flags().GetBool(flagNoOptionalName)
		if withOptional && noOptional {
			return fmt.Errorf("--with-optional and --no-optional cannot be used together")
		}
		return nil
	},
	RunE: runWithErrorLogging(actionAdd),
}

func init() {
	addCmd.Flags().BoolP(
		flagForceName,
		"f",
		false,
		"Ignore version, dependency, and platform warnings",
	)
	addCmd.Flags().Bool(
		flagWithOptionalName,
		false,
		"Also install optional upstream dependencies",
	)
	addCmd.Flags().Bool(
		flagNoOptionalName,
		false,
		"Skip optional upstream dependencies (default)",
	)
	addSourceFlag(addCmd)
	addNoStyleFlag(addCmd)
	rootCmd.AddCommand(addCmd)
}

func actionAdd(cmd *cobra.Command, args []string) error {
	workspace, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("unable to get current directory: %w", err)
	}

	stateSvc := state.NewProjectStateService(workspace)
	hasLucyState, err := lucyStateDirExists(workspace)
	if err != nil {
		return err
	}
	if hasLucyState {
		if err := stateSvc.Load(cmd.Context()); err != nil {
			return fmt.Errorf("load lucy state: %w", err)
		}
		log.ShowInfo(formatStateSummary(stateSvc))
	}

	withOptional, _ := cmd.Flags().GetBool(flagWithOptionalName)
	force, _ := cmd.Flags().GetBool(flagForceName)
	source, _ := cmd.Flags().GetString("source")

	options := install.DefaultOptions()
	options.WithOptional = withOptional
	options.Force = force

	requests := make([]types.PackageRequest, 0, len(args))
	for _, arg := range args {
		req, err := packageRequestFromInput(arg, source)
		if err != nil {
			return fmt.Errorf("stopping package addition: %w", err)
		}
		requests = append(requests, req)
	}

	var result *install.Result
	if len(requests) > 1 {
		result, err = install.InstallMany(cmd.Context(), requests, options)
	} else {
		req := requests[0]
		if req.Version == types.VersionAny {
			req.Version = types.VersionCompatible
		}
		result, err = install.Install(cmd.Context(), req, options)
	}
	if err != nil {
		var conflictErr *resolve.ConstraintConflictError
		if errors.As(err, &conflictErr) {
			return formatConstraintConflict(conflictErr)
		}
		return err
	}

	if !hasLucyState {
		return nil
	}

	if err := updateAddState(
		cmd.Context(),
		workspace,
		stateSvc,
		requests,
		result,
	); err != nil {
		return fmt.Errorf("update state: %w", err)
	}

	return nil
}

func lucyStateDirExists(workDir string) (bool, error) {
	info, err := os.Stat(filepath.Join(workDir, "lucy.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat lucy.yaml: %w", err)
	}
	return !info.IsDir(), nil
}

func formatStateSummary(stateSvc *state.ProjectStateService) string {
	status := []string{
		presenceLabel(
			"config",
			stateSvc.Manifest() != nil && stateSvc.Manifest().Config != nil,
		),
		presenceLabel("manifest", stateSvc.Manifest() != nil),
		presenceLabel("lock", stateSvc.Lock() != nil),
	}
	return "Lucy state: " + strings.Join(status, ", ")
}

func presenceLabel(name string, present bool) string {
	if present {
		return name + " present"
	}
	return name + " absent"
}

func updateAddState(
	ctx context.Context,
	workDir string,
	stateSvc *state.ProjectStateService,
	requests []types.PackageRequest,
	result *install.Result,
) error {
	if stateSvc == nil {
		return nil
	}

	manifestIntent := buildUpdatedManifest(stateSvc.Manifest(), requests)
	if result == nil || len(result.Installed) == 0 {
		return stateSvc.Save(ctx, manifestIntent, nil)
	}

	lock := buildUpdatedLock(workDir, manifestIntent, stateSvc.Lock(), result)
	manifest := state.UpdateManifestRolesForAdd(
		stateSvc.Manifest(),
		requests,
		lock,
	)
	return stateSvc.Save(ctx, manifest, lock)
}

func formatConstraintConflict(err *resolve.ConstraintConflictError) error {
	if err == nil {
		return fmt.Errorf("dependency constraints conflict")
	}

	return fmt.Errorf(
		"dependency constraints conflict for %s: %s requires %s, %s requires %s",
		err.PackageId.StringBase(),
		err.Left.Requester,
		resolve.FormatVersionConstraint(err.Left.Constraint),
		err.Right.Requester,
		resolve.FormatVersionConstraint(err.Right.Constraint),
	)
}

func buildUpdatedManifest(
	existing *state.Manifest,
	requests []types.PackageRequest,
) *state.Manifest {
	manifest := existing
	for _, req := range requests {
		manifest = state.UpsertManifestRequiredIntent(
			manifest,
			req,
			req.Scope.String(),
		)
	}
	return manifest
}

func buildUpdatedLock(
	workDir string,
	manifest *state.Manifest,
	existing *state.Lock,
	result *install.Result,
) *state.Lock {
	var lock state.Lock
	if existing != nil {
		lock = *existing
		lock.Bundles = append([]state.LockedBundle(nil), existing.Bundles...)
		lock.Packages = append([]state.LockedPackage(nil), existing.Packages...)
	} else {
		lock = state.NewLock()
	}

	serverInfo := workspace.ServerInfo()
	runtime := serverInfo.Runtime
	lock.GeneratedAt = state.NewLock().GeneratedAt
	lock.ManifestFingerprint = manifestFingerprint(
		manifest,
		lock.ManifestFingerprint,
	)
	lock.GameVersion = manifestGameVersion(manifest, runtime, lock.GameVersion)
	lock.Platform = manifestPlatform(manifest, serverInfo, lock.Platform)
	lock.PlatformVersion = manifestPlatformVersion(
		manifest,
		serverInfo,
		lock.PlatformVersion,
	)

	packagesByID := make(
		map[string]state.LockedPackage,
		len(lock.Packages)+len(result.Installed),
	)
	for _, pkg := range lock.Packages {
		packagesByID[pkg.ID] = pkg
	}
	for _, pkg := range result.Installed {
		locked := lockedPackageFromInstalled(
			workDir,
			pkg,
			result.Provenance[pkg.Id.StringBase()],
		)
		packagesByID[locked.ID] = locked
	}
	packages := make([]state.LockedPackage, 0, len(packagesByID))
	for _, pkg := range packagesByID {
		packages = append(packages, pkg)
	}
	lock.Packages = state.CanonicalLockedPackages(packages)

	return &lock
}

func manifestFingerprint(manifest *state.Manifest, fallback string) string {
	if manifest != nil {
		data, err := state.SerializeManifest(manifest)
		if err == nil {
			sum := sha256.Sum256(data)
			return "sha256:" + hex.EncodeToString(sum[:])
		}
	}
	if fallback != "" {
		return fallback
	}
	return "sha256:absent"
}

func manifestGameVersion(
	manifest *state.Manifest,
	runtime *workspace.ServerRuntime,
	fallback string,
) string {
	if manifest != nil && manifest.Environment.GameVersion != "" {
		return manifest.Environment.GameVersion
	}
	if runtime != nil {
		if version := runtime.GameVersion.String(); version != "" {
			return version
		}
	}
	if fallback != "" {
		return fallback
	}
	return types.VersionUnknown.String()
}

func manifestPlatform(
	manifest *state.Manifest,
	serverInfo workspace.Workspace,
	fallback string,
) string {
	if manifest != nil && manifest.Environment.ModdingPlatform != "" {
		return manifest.Environment.ModdingPlatform
	}
	if serverInfo.Runtime != nil {
		if platform := serverInfo.DerivedModLoader().String(); platform != "" {
			return platform
		}
	}
	if fallback != "" {
		return fallback
	}
	return string(types.PlatformNone)
}

func manifestPlatformVersion(
	manifest *state.Manifest,
	serverInfo workspace.Workspace,
	fallback string,
) string {
	if manifest != nil && manifest.Environment.ModdingPlatformVersion != "" {
		return manifest.Environment.ModdingPlatformVersion
	}
	if serverInfo.Runtime != nil {
		if version := serverInfo.DerivedLoaderVersion(); version != "" {
			return version
		}
	}
	if fallback != "" {
		return fallback
	}
	return types.VersionUnknown.String()
}

func lockedPackageFromInstalled(
	workDir string,
	pkg types.InstalledPackage,
	provenance []string,
) state.LockedPackage {
	requester := "root"
	if len(provenance) > 0 {
		requester = provenance[len(provenance)-1]
	}

	source := "direct"
	hash := "unknown"
	hashAlgorithm := "sha1"
	if src := pkg.Id.Scope.String(); src != "unknown" {
		source = src
	}
	filename := filepath.Base(pkg.Path)
	if pkg.Filename != "" {
		filename = pkg.Filename
	}
	if pkg.Hash != "" {
		hash = pkg.Hash
	}
	if pkg.HashAlgorithm != "" {
		hashAlgorithm = pkg.HashAlgorithm
	}

	return state.LockedPackage{
		ID:            pkg.Id.StringBase(),
		Version:       pkg.Id.Version.String(),
		Source:        source,
		URL:           pkg.FileUrl,
		Filename:      filename,
		Hash:          hash,
		HashAlgorithm: hashAlgorithm,
		InstallPath:   relativeInstallPath(workDir, pkg.Path),
		Side:          string(state.SideBoth),
		Provenance:    normalizedProvenance(provenance),
		Requester:     requester,
	}
}

func relativeInstallPath(workDir, installPath string) string {
	if installPath == "" {
		return ""
	}
	if rel, err := filepath.Rel(workDir, installPath); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(installPath)
}

func normalizedProvenance(provenance []string) []string {
	if len(provenance) == 0 {
		return []string{"root"}
	}
	return append([]string(nil), provenance...)
}
