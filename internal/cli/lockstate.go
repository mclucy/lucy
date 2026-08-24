package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
)

// LucyStateDirExists reports whether a lucy.yaml manifest exists in workDir.
func LucyStateDirExists(workDir string) (bool, error) {
	info, err := os.Stat(filepath.Join(workDir, "lucy.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat lucy.yaml: %w", err)
	}
	return !info.IsDir(), nil
}

// FormatConstraintConflict rewraps a resolver constraint conflict with the
// package id and both sides of the conflict.
func FormatConstraintConflict(err *resolve.ConstraintConflictError) error {
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

// BuildUpdatedLock merges freshly installed packages into an existing lock,
// or builds a new one when no lock exists yet.
func BuildUpdatedLock(
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

	ws := workspace.New()
	runtime := ws.Server
	lock.GeneratedAt = state.NewLock().GeneratedAt
	lock.ManifestFingerprint = ManifestFingerprint(
		manifest,
		lock.ManifestFingerprint,
	)
	lock.GameVersion = manifestGameVersion(manifest, runtime, lock.GameVersion)
	lock.Platform = manifestEcosystem(manifest, ws, lock.Platform)
	lock.PlatformVersion = manifestEcosystemVersion(
		manifest,
		ws,
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

// ManifestFingerprint fingerprints the canonical serialized manifest,
// falling back to the provided fallback when serialization fails.
func ManifestFingerprint(manifest *state.Manifest, fallback string) string {
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
	runtime *workspace.ServerInstance,
	fallback string,
) string {
	if manifest != nil && manifest.Environment.GameVersion != "" {
		return manifest.Environment.GameVersion
	}
	if runtime != nil {
		if version := runtime.GameVersion().String(); version != "" {
			return version
		}
	}
	if fallback != "" {
		return fallback
	}
	return types.VersionUnknown.String()
}

func manifestEcosystem(
	manifest *state.Manifest,
	ws workspace.Workspace,
	fallback string,
) string {
	if manifest != nil && manifest.Environment.ModdingPlatform != "" {
		return manifest.Environment.ModdingPlatform
	}
	if ws.Server != nil {
		if platform := ws.DerivedModLoader().String(); platform != "" {
			return platform
		}
	}
	if fallback != "" {
		return fallback
	}
	return string(types.EcoUnspecified)
}

func manifestEcosystemVersion(
	manifest *state.Manifest,
	ws workspace.Workspace,
	fallback string,
) string {
	if manifest != nil && manifest.Environment.ModdingPlatformVersion != "" {
		return manifest.Environment.ModdingPlatformVersion
	}
	if ws.Server != nil {
		if version := ws.DerivedLoaderVersion(); version != "" {
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
