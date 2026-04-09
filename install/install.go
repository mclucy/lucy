package install

import (
	"errors"
	"fmt"
	"os"

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
	if id.Version == types.VersionAny {
		id.Version = types.VersionCompatible
	}

	// route to platform installer if it's an identity package
	if id.IsIdentityPackage() {
		return installPlatform(id)
	}

	// this is order-sensitive, ensureServerPlatformMatch() does not check for
	// identity packages
	if err := ensureServerPlatformMatch(id); err != nil {
		return err
	}

	p := id.NewPackage()
	serverInfo := probe.ServerInfo()

	// Create server work path if it doesn't exist and is not "."
	workPath := serverInfo.WorkPath
	if workPath != "." {
		if err := os.MkdirAll(workPath, 0o755); err != nil {
			return fmt.Errorf("create server work path failed: %w", err)
		}
	}

	serverPlatform := serverInfo.Runtime.DerivedModLoader()
	hasMcdr := serverInfo.Environments.Mcdr != nil

	providers, err := routing.ResolveProvidersByTopology(
		serverInfo.Runtime.Topology,
		serverPlatform,
		source,
	)
	if err != nil {
		return err
	}

	if hasMcdr {
		mcdrProviders, err := routing.ResolveProviders(
			types.PlatformMCDR,
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

	fetches, errs := routing.FetchMany(providers, id)

	// Only report provider errors if no successful results
	if len(fetches) == 0 {
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
	}

	switch len(fetches) {
	case 0:
		return fmt.Errorf("no candidates found for %s", id.String())
	case 1:
		// good,follow through
		p.Id = fetches[0].ResolvedID
		p.Remote = &fetches[0].Remote
	default:
		// prompt user to select one
		selected, err := selectFromCandidates(fetches)
		if err != nil {
			return err
		}
		p.Id = selected.ResolvedID
		p.Remote = &selected.Remote
	}
	source = p.Remote.Source
	resolvedPlatform := p.Id.Platform

	installer := installers[resolvedPlatform]
	if installer == nil {
		return fmt.Errorf("no installer found for platform %s", resolvedPlatform)
	}
	err = installer(p)
	if err != nil {
		return err
	}

	return nil
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

func selectFromCandidates(candidates []upstream.FetchResult) (
	selected *upstream.FetchResult,
	err error,
) {
	options := make([]huh.Option[upstream.FetchResult], len(candidates))
	for i, candidate := range candidates {
		options[i] = huh.NewOption(
			candidate.Remote.Source.Title()+" "+candidate.Remote.Filename,
			candidate,
		)
	}
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[upstream.FetchResult]().
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
