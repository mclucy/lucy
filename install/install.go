package install

import (
	"errors"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
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

	if err := ensurePlatformMatch(id); err != nil {
		return err
	}

	serverInfo := probe.ServerInfo()
	serverPlatform := serverInfo.Executable.ModLoader
	providerPlatform := resolveProviderPlatform(id, serverPlatform)
	hasMcdr := serverInfo.Environments.Mcdr != nil

	if !shouldSkipRemoteFetch(id) {
		providers, err := routing.ResolveProviders(providerPlatform, source)
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
	}

	installer := installers[id.Platform]
	if installer == nil {
		return fmt.Errorf("no installer found for platform %s", id.Platform)
	}
	err := installer(p)
	if err != nil {
		return err
	}

	return nil
}

func ensurePlatformMatch(id types.PackageId) error {
	platform := id.Platform
	serverInfo := probe.ServerInfo()

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
		if isForgeInstallerPackage(id.Name) && serverInfo.Executable.ModLoader == types.Minecraft {
			return nil
		}
		if serverInfo.Executable.ModLoader != types.Forge {
			return errors.New("forge server not found")
		}
		return nil
	case types.Fabric:
		if serverInfo.Executable == probe.UnknownExecutable {
			return errors.New("no executable found, `lucy add` requires a server in current directory")
		}
		if isFabricLoaderPackage(id.Name) && serverInfo.Executable.ModLoader == types.Minecraft {
			return nil
		}
		if serverInfo.Executable.ModLoader != types.Fabric {
			return errors.New("fabric server not found")
		}
		return nil
	case types.Neoforge:
		if serverInfo.Executable == probe.UnknownExecutable {
			return errors.New("no executable found, `lucy add` requires a server in current directory")
		}
		if serverInfo.Executable.ModLoader != types.Neoforge {
			return errors.New("neoforge server not found")
		}
		return nil
	default:
		return errors.New("unsupported platform")
	}
}

func isFabricLoaderPackage(name types.ProjectName) bool {
	return name == types.ProjectName(types.Fabric) || name == "fabric-loader"
}

func isForgeInstallerPackage(name types.ProjectName) bool {
	return name == types.ProjectName(types.Forge) || name == "minecraftforge"
}

func resolveProviderPlatform(
	id types.PackageId,
	serverPlatform types.Platform,
) types.Platform {
	if id.Platform == types.Fabric && isFabricLoaderPackage(id.Name) {
		return types.Fabric
	}
	if id.Platform == types.Forge && isForgeInstallerPackage(id.Name) {
		return types.Forge
	}
	return serverPlatform
}

func shouldSkipRemoteFetch(id types.PackageId) bool {
	return (id.Platform == types.Fabric && isFabricLoaderPackage(id.Name)) ||
		(id.Platform == types.Forge && isForgeInstallerPackage(id.Name))
}

func fetchFromSources(id types.PackageId, sources []upstream.Provider) (
	candidates []upstream.RawPackageRemote,
	err error,
) {
	for _, src := range sources {
		remoteData, err := src.Fetch(id)
		if err != nil {
			logger.ShowInfo(err)
			err = nil
			continue
		}
		if remoteData != nil {
			candidates = append(candidates, remoteData)
		}
	}

	return candidates, nil
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
