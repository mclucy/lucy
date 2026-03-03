package install

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"
)

type platformInstaller func(p types.Package) error

var installers = map[types.Platform]platformInstaller{}

func registerInstaller(platform types.Platform, installer platformInstaller) {
	if installer == nil {
		panic("install: nil installer")
	}
	installers[platform] = installer
}

func Install(id types.PackageId, source types.Source) error {
	p := id.NewPackage()

	// route to platform installer if it's an identity package
	if id.IsIdentityPackage() {
		return installPlatform(id)
	}

	// this is order-sensitive, ensureServerPlatformMatch() does not check for
	// identity packages
	if err := ensureServerPlatformMatch(id); err != nil {
		return err
	}

	serverInfo := probe.ServerInfo()
	serverPlatform := serverInfo.Executable.ModLoader
	hasMcdr := serverInfo.Environments.Mcdr != nil

	providers, err := routing.ResolveProviders(serverPlatform, source)
	if err != nil {
		return err
	}

	if hasMcdr {
		mcdrProviders, err := routing.ResolveProviders(
			types.Mcdr,
			types.SourceAuto,
		)
		if err != nil {
			logger.ShowInfo(
				fmt.Errorf(
					"failed to resolve MCDR provider: %w",
					err,
				),
			)
		}
		providers = append(providers, mcdrProviders...)
	}

	remotes, errs := routing.FetchMany(providers, id)
	for _, err := range errs {
		if source == types.SourceAuto && len(providers) > 1 {
			logger.ReportWarn(
				fmt.Errorf(
					"search on %s failed: %w",
					err.Source.Title(),
					err.Err,
				),
			)
			continue
		}
	}

	switch len(remotes) {
	case 0:
		return fmt.Errorf("no candidates found for %s", id.String())
	case 1:
		// good,follow through
		p.Remote = &remotes[0]
	default:
		// prompt user to select one
		var err error
		p.Remote, err = selectFromCandidates(remotes)
		if err != nil {
			return err
		}
	}
	source = p.Remote.Source

	installer := installers[id.Platform]
	if installer == nil {
		return fmt.Errorf("no installer found for platform %s", id.Platform)
	}
	err = installer(p)
	if err != nil {
		return err
	}

	return nil
}

func installPlatform(id types.PackageId) error {
	serverInfo := probe.ServerInfo()
	serverPlatform := serverInfo.Executable.ModLoader
	hasMcdr := serverInfo.Environments.Mcdr != nil

	err := id.IsValidIdentityPackage()
	if err != nil {
		return err
	}

	errExistingPlatform := func() error {
		if serverPlatform != types.UnknownPlatform {
			return fmt.Errorf(
				"found an existing server platform %s, installation of %s aborted",
				serverPlatform.Title(),
				id.Platform.Title(),
			)
		}
		return nil
	}

	id.NormalizeIdentityPackage()
	switch id.IdentityToPlatform() {
	case types.Minecraft:
		if serverPlatform != types.UnknownPlatform {
			// TODO: ask if overwrite existing server
			return errors.New("minecraft already installed")
		}
		return installMinecraftServer(id)
	case types.Forge:
		if serverPlatform != types.Vanilla {
			// TODO: ask if overwrite existing modding platform
			return errExistingPlatform()
		}
		return installForge(id)
	case types.Fabric:
		if serverPlatform != types.Vanilla {
			// TODO: ask if overwrite existing modding platform
			return errExistingPlatform()
		}
		return installFabric(id)
	case types.Neoforge:
		if serverPlatform != types.Vanilla {
			return errExistingPlatform()
		}
		return installNeoForge(id)
	case types.Mcdr:
		if hasMcdr {
			return errors.New("mcdr already installed")
		}
		return initMcdr()
	default:
		return fmt.Errorf("cannot install platform: %s", id.Platform)
	}
}

func ensureServerPlatformMatch(id types.PackageId) error {
	platform := id.Platform
	serverInfo := probe.ServerInfo()
	serverPlatform := serverInfo.Executable.ModLoader

	switch platform {
	case types.AllPlatform:
		return nil
	case types.Mcdr:
		if serverInfo.Environments.Mcdr == nil {
			return errors.New("mcdr not found")
		}
		return nil
	case types.Forge:
		if serverInfo.Executable == probe.UnknownExecutable {
			return errors.New("no executable found, `lucy add` requires a server in current directory")
		}
		if serverPlatform != types.Forge {
			return errors.New("forge server not found")
		}
		return nil
	case types.Fabric:
		if serverInfo.Executable == probe.UnknownExecutable {
			return errors.New("no executable found, `lucy add` requires a server in current directory")
		}
		if serverPlatform != types.Fabric {
			return errors.New("fabric server not found")
		}
		return nil
	case types.Neoforge:
		if serverInfo.Executable == probe.UnknownExecutable {
			return errors.New("no executable found, `lucy add` requires a server in current directory")
		}
		if serverPlatform != types.Neoforge {
			return errors.New("neoforge server not found")
		}
		return nil
	default:
		return errors.New("unsupported platform")
	}
}

func selectFromCandidates(candidates []types.PackageRemote) (
	selected *types.PackageRemote,
	err error,
) {
	options := make([]huh.Option[types.PackageRemote], len(candidates))
	for i, candidate := range candidates {
		options[i] = huh.NewOption(
			candidate.Source.Title()+" "+candidate.Filename,
			candidate,
		)
	}
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[types.PackageRemote]().
				Title("Multiple candidates found, please select one").
				Options(options...).
				Value(selected),
		),
	).Run()

	if err != nil {
		return nil, err
	}

	return selected, nil
}
