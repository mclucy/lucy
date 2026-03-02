package install

import (
	"errors"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/remote"
	"github.com/mclucy/lucy/remote/source"
	"github.com/mclucy/lucy/types"
)

type platformInstaller func(p types.Package) error

var installers = map[types.Platform]platformInstaller{}

func registerInstaller(platform types.Platform, installer platformInstaller) {
	if installer == nil {
		panic("install: nil installer")
	}
	installers[platform] = installer
}

func Install(id types.PackageId, designatedSource types.Source) error {
	p := id.NewPackage()

	// serverInfo := probe.ServerInfo()
	// severPlatform := serverInfo.Executable.ModLoader
	// hasMcdr := serverInfo.Environments.Mcdr != nil

	sources := source.All // TODO: filter sources based on platform and user preference

	remoteCandidates, err := fetchFromSources(id, sources)
	if err != nil {
		return err
	}
	if remoteCandidates == nil {
		return errors.New("package not found in any source")
	}

	// TODO: prompt user when multiple matches exists on different sources.
	remoteData := remoteCandidates[0]

	r := remoteData.ToPackageRemote()
	p.Remote = &r

	return nil

	// installer, ok := getInstaller()
	// return installer()
}

func ensurePlatformMatch(platform types.Platform) error {
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
		if serverInfo.Executable.ModLoader != types.Forge {
			return errors.New("forge server not found")
		}
		return nil
	case types.Fabric:
		if serverInfo.Executable == probe.UnknownExecutable {
			return errors.New("no executable found, `lucy add` requires a server in current directory")
		}
		if serverInfo.Executable.ModLoader != types.Fabric {
			return errors.New("fabric server not found")
		}
		return nil
	default:
		return errors.New("unsupported platform")
	}
}

func fetchFromSources(id types.PackageId, sources []remote.SourceHandler) (
	candidates []remote.RawPackageRemote,
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

func selectFromCandidates(candidates []remote.RawPackageRemote) (
	remote.RawPackageRemote,
	error,
) {
	panic("not implemented yet")
}
