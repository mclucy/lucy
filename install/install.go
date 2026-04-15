package install

import (
	"errors"
	"fmt"

	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
)

type platformInstaller func(p types.Package) error

type Result struct {
	Installed  []types.Package
	Provenance map[string][]string
}

var installers = map[types.Platform]platformInstaller{}

func registerInstaller(platform types.Platform, installer platformInstaller) {
	if installer == nil {
		panic("install: nil installer")
	}
	installers[platform] = installer
}

func Install(id types.PackageId, source types.Source, options Options) (*Result, error) {
	// for regular (non-identity) packages, delegate to InstallMany to unify
	// resolver behavior with batch adds
	if !id.IsIdentityPackage() {
		return InstallMany([]types.PackageId{id}, source, options)
	}

	// identity packages go through the established platform installer
	if id.Version == types.VersionAny {
		id.Version = types.VersionCompatible
	}

	if err := installPlatform(id); err != nil {
		return nil, err
	}

	return &Result{}, nil
}

func installPlatform(id types.PackageId) error {
	id.NormalizeIdentityPackage()
	err := id.IsValidIdentityPackage()
	if err != nil {
		return err
	}

	serverInfo := probe.ServerInfo()
	serverPlatform := serverInfo.Runtime.DerivedModLoader()
	hasMcdr := serverInfo.Environments.Mcdr != nil

	errExistingPlatform := func() error {
		return fmt.Errorf(
			"found an existing server platform %s, installation of %s aborted",
			serverPlatform.Title(),
			id.Platform.Title(),
		)
	}

	switch id.IdentityToPlatform() {
	case types.PlatformMinecraft:
		if serverPlatform != types.PlatformNone {
			// TODO: ask if overwrite existing server
			return errors.New("a server is already installed")
		}
		return installMinecraftServer(id)
	case types.PlatformForge:
		switch serverPlatform {
		case types.PlatformVanilla, types.PlatformNone:
			return installForge(id)
		default:
			return errExistingPlatform()
		}

	case types.PlatformFabric:
		switch serverPlatform {
		case types.PlatformUnknown:
			return errors.New("unknown mod loader, cannot infer fabric bootstrap artifact")
		case types.PlatformFabric:
			return errors.New("fabric server already detected, installation aborted")
		case types.PlatformForge:
			return errors.New("Forge server detected, cannot install Fabric bootstrap")
		case types.PlatformNeoforge:
			return errors.New("NeoForge server detected, cannot install Fabric bootstrap")
		case types.PlatformVanilla:
			override, deleteVanilla := promptOverrideVanillaWithFabric()
			if !override {
				return errors.New("installation aborted by user")
			}
			return installFabricWithOverride(id, deleteVanilla)
		case types.PlatformNone:
		default:
			return fmt.Errorf(
				"unsupported server platform %s for fabric installation",
				serverPlatform.Title(),
			)
		}
		return installFabric(id)
	case types.PlatformNeoforge:
		switch serverPlatform {
		case types.PlatformVanilla, types.PlatformNone:
			return installNeoForge(id)
		default:
			return errExistingPlatform()
		}
	case types.PlatformMCDR:
		if hasMcdr {
			return errors.New("mcdr already installed")
		}
		return initMcdr()
	default:
		return fmt.Errorf("cannot install platform: %s", id.Platform)
	}
}
