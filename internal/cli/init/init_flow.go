// Package init writes a Lucy manifest for an existing server directory.
//
// Server creation lives in internal/cli/create. Init reads the directory,
// writes lucy.yaml, and does not touch the lock file.
package init

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
)

// Options carries the inputs of the init command.
type Options struct {
	// GameVersion fills the manifest when no server runtime exists.
	// Empty without the flag.
	GameVersion string

	// Force overwrites an existing manifest without asking.
	Force bool

	// AllowEmpty allows generating a manifest of an empty server
	AllowEmpty bool
}

// ManifestExists reports whether lucy.yaml is already present in dir.
func ManifestExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, string(state.ManifestFile)))
	return err == nil
}

// EmptyServerManifest builds a manifest for a directory with no server.
func EmptyServerManifest(gameVersion string) *state.Manifest {
	mf := state.ManifestDefaults()
	mf.Environment.GameVersion = gameVersion
	return &mf
}

// ManifestFromDetection builds a manifest from one resolved server
// instance. Unknown versions stay empty. Nothing gets guessed.
func ManifestFromDetection(ws workspace.Workspace) *state.Manifest {
	mf := state.ManifestDefaults()
	server := ws.Server()

	if v := server.GameVersion(); concreteVersion(v) {
		mf.Environment.GameVersion = v.String()
	}

	platform := server.ModLoader()
	switch platform {
	case types.EcoFabric, types.EcoForge, types.EcoNeoforge:
		mf.Environment.ModdingPlatform = platform.String()
		mf.Environment.ModdingPlatformVersion = componentVersion(
			server.RuntimeComponents,
			platform,
		)
	}

	if core := server.ServerCore(); core != "" &&
		core != "minecraft" &&
		core != platform.String() {
		mf.Environment.ServerCore = core
		if server.PrimaryRuntime != nil &&
			concreteVersion(server.PrimaryRuntime.Version) {
			mf.Environment.ServerCoreVersion = server.PrimaryRuntime.Version.String()
		}
	}

	mf.Environment.Mcdr = ws.Environments.Mcdr != nil
	return &mf
}

// SaveManifest writes the manifest. Then it refreshes the state for dir.
func SaveManifest(dir string, mf *state.Manifest) error {
	service := state.NewProjectStateService(dir)
	if err := service.Save(context.Background(), mf, nil); err != nil {
		return err
	}
	workspace.Refresh(dir)
	return nil
}

// componentVersion returns the version of the first component for eco.
// It returns "" with no match.
func componentVersion(
	components []types.VersionedPackageRef,
	eco types.Ecosystem,
) string {
	for _, component := range components {
		if component.Eco == eco && concreteVersion(component.Version) {
			return component.Version.String()
		}
	}
	return ""
}

// concreteVersion reports whether version names one exact release.
// Selectors and markers return false.
func concreteVersion(version types.BareVersion) bool {
	switch version {
	case "", types.VersionNone, types.VersionUnknown, types.VersionAny,
		types.VersionStable, types.VersionBeta:
		return false
	default:
		return true
	}
}
