package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/types"
	"github.com/spf13/cobra"
)

const (
	flagForceName        = "force"
	flagWithOptionalName = "with-optional"
	flagNoOptionalName   = "no-optional"
)

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add new mods, plugins, or server modules",
	Args:  cobra.MinimumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return CompletePackageIDSuggestions(context.Background(), "add", toComplete)
	},
	PreRunE: func(cmd *cobra.Command, args []string) error {
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
	addCmd.Flags().BoolP(flagForceName, "f", false, "Ignore version, dependency, and platform warnings")
	addCmd.Flags().Bool(flagWithOptionalName, false, "Also install optional upstream dependencies")
	addCmd.Flags().Bool(flagNoOptionalName, false, "Skip optional upstream dependencies (default)")
	addNoStyleFlag(addCmd)
	rootCmd.AddCommand(addCmd)
}

func actionAdd(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}

	stateSvc := state.NewProjectStateService(workDir)
	hasLucyState, err := lucyStateDirExists(workDir)
	if err != nil {
		return err
	}
	if hasLucyState {
		if err := stateSvc.Load(cmd.Context()); err != nil {
			return fmt.Errorf("load lucy state: %w", err)
		}
		logger.ShowInfo(formatStateSummary(stateSvc))
	}

	withOptional, _ := cmd.Flags().GetBool(flagWithOptionalName)

	options := install.DefaultOptions()
	options.WithOptional = withOptional

	ids := make([]types.PackageId, 0, len(args))
	for _, arg := range args {
		id, err := syntax.Parse(arg)
		if err != nil {
			logger.Fatal(err)
		}
		ids = append(ids, id)
	}

	var result *install.Result
	if len(ids) > 1 {
		result, err = install.InstallMany(ids, types.SourceAuto, options)
	} else {
		id := ids[0]
		if id.Version == types.VersionAny {
			id.Version = types.VersionCompatible
		}
		result, err = install.Install(id, types.SourceAuto, options)
	}
	if err != nil {
		return err
	}

	if !hasLucyState || result == nil || len(result.Installed) == 0 {
		return nil
	}

	if err := updateAddLockfile(workDir, stateSvc, result); err != nil {
		return fmt.Errorf("update lock: %w", err)
	}

	return nil
}

func lucyStateDirExists(workDir string) (bool, error) {
	info, err := os.Stat(filepath.Join(workDir, ".lucy"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat .lucy directory: %w", err)
	}
	return info.IsDir(), nil
}

func formatStateSummary(stateSvc *state.ProjectStateService) string {
	status := []string{
		presenceLabel("config", stateSvc.Config() != nil),
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

func updateAddLockfile(workDir string, stateSvc *state.ProjectStateService, result *install.Result) error {
	if stateSvc == nil || result == nil || len(result.Installed) == 0 {
		return nil
	}

	lock := buildUpdatedLock(workDir, stateSvc, result)
	return state.WriteLock(workDir, lock)
}

func buildUpdatedLock(workDir string, stateSvc *state.ProjectStateService, result *install.Result) *state.Lock {
	var lock state.Lock
	if existing := stateSvc.Lock(); existing != nil {
		lock = *existing
		lock.Packages = append([]state.LockedPackage(nil), existing.Packages...)
		lock.Bundles = append([]state.LockedBundle(nil), existing.Bundles...)
	} else {
		lock = state.NewLock()
	}

	runtime := probe.ServerInfo().Runtime
	lock.GeneratedAt = state.NewLock().GeneratedAt
	lock.ManifestFingerprint = manifestFingerprint(stateSvc.Manifest(), lock.ManifestFingerprint)
	lock.GameVersion = manifestGameVersion(stateSvc.Manifest(), runtime, lock.GameVersion)
	lock.Platform = manifestPlatform(stateSvc.Manifest(), runtime, lock.Platform)
	lock.PlatformVersion = manifestPlatformVersion(stateSvc.Manifest(), runtime, lock.PlatformVersion)

	for _, pkg := range result.Installed {
		locked := lockedPackageFromInstalled(workDir, pkg, result.Provenance[pkg.Id.StringPlatformName()])
		upsertLockedPackage(&lock, locked)
	}

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

func manifestGameVersion(manifest *state.Manifest, runtime *types.RuntimeInfo, fallback string) string {
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

func manifestPlatform(manifest *state.Manifest, runtime *types.RuntimeInfo, fallback string) string {
	if manifest != nil && manifest.Environment.Platform != "" {
		return manifest.Environment.Platform
	}
	if runtime != nil {
		if platform := runtime.DerivedModLoader().String(); platform != "" {
			return platform
		}
	}
	if fallback != "" {
		return fallback
	}
	return string(types.PlatformNone)
}

func manifestPlatformVersion(manifest *state.Manifest, runtime *types.RuntimeInfo, fallback string) string {
	if manifest != nil && manifest.Environment.PlatformVersion != "" {
		return manifest.Environment.PlatformVersion
	}
	if runtime != nil {
		if version := runtime.DerivedLoaderVersion(); version != "" {
			return version
		}
	}
	if fallback != "" {
		return fallback
	}
	return types.VersionUnknown.String()
}

func lockedPackageFromInstalled(workDir string, pkg types.Package, provenance []string) state.LockedPackage {
	requester := "root"
	if len(provenance) > 0 {
		requester = provenance[len(provenance)-1]
	}

	installPath := ""
	filename := ""
	if pkg.Local != nil {
		filename = filepath.Base(pkg.Local.Path)
		installPath = relativeInstallPath(workDir, pkg.Local.Path)
	}

	source := "direct"
	url := ""
	hash := "unknown"
	hashAlgorithm := "sha1"
	if pkg.Remote != nil {
		if src := pkg.Remote.Source.String(); src != "unknown" {
			source = src
		}
		url = pkg.Remote.FileUrl
		if pkg.Remote.Filename != "" {
			filename = pkg.Remote.Filename
		}
		if pkg.Remote.Hash != "" {
			hash = pkg.Remote.Hash
		}
		if pkg.Remote.HashAlgorithm != "" {
			hashAlgorithm = pkg.Remote.HashAlgorithm
		}
	}

	return state.LockedPackage{
		ID:            pkg.Id.StringPlatformName(),
		Version:       pkg.Id.Version.String(),
		Source:        source,
		URL:           url,
		Filename:      filename,
		Hash:          hash,
		HashAlgorithm: hashAlgorithm,
		InstallPath:   installPath,
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

func upsertLockedPackage(lock *state.Lock, pkg state.LockedPackage) {
	for i := range lock.Packages {
		if lock.Packages[i].ID == pkg.ID {
			lock.Packages[i] = pkg
			return
		}
	}
	lock.Packages = append(lock.Packages, pkg)
}
