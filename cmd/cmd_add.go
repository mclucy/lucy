package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/remote"
	"github.com/mclucy/lucy/remote/source"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/util"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/types"

	"github.com/urfave/cli/v3"
)

var subcmdAdd = &cli.Command{
	Name:  "add",
	Usage: "Add new mods, plugins, or server modules",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "force",
			Aliases: []string{"f"},
			Usage:   "Ignore version, dependency, and platform warnings",
			Value:   false,
		},
		&cli.StringFlag{
			Name:    "source",
			Aliases: []string{"s"},
			Usage:   "Specify the source to download from (modrinth, mcdr)",
			Value:   "none",
		},
		flagNoStyle,
	},
	Action: tools.Decorate(
		actionAdd,
		decoratorGlobalFlags,
		decoratorHelpAndExitOnNoArg,
	),
}

var actionAdd cli.ActionFunc = func(
	ctx context.Context,
	cmd *cli.Command,
) error {
	// get id from args
	id := syntax.Parse(cmd.Args().First())

	// probe server info
	serverInfo := probe.ServerInfo()

	// ensure we are in a lucy-managed server
	// TODO: Disabled for now, the part for building the program directory is not done
	// if !serverInfo.HasLucy {
	// 	return errors.New("lucy is not installed, run `lucy init` before downloading mods")
	// }

	if serverInfo.Executable == probe.UnknownExecutable {
		return errors.New("no executable found, `lucy add` requires a server in current directory")
	}

	// check if the specified platform matches the server platform
	if id.Platform != types.AllPlatform {
		if id.Platform == types.Mcdr {
			// for mcdr, we only need to check if it's mcdr-managed
			if serverInfo.Environments.Mcdr == nil {
				return errors.New("mcdr not found")
			}
		} else if id.Platform != serverInfo.Executable.ModLoader {
			return errors.New("platform mismatch")
		}
	}

	// Get the appropriate directory to download file to.
	// This is a temporary solution. Installation is not supposed to be this simple.
	// The installer should be designed as an injectable interface to allow non-standard
	// installation methods.
	var dir string
	switch id.Platform {
	case types.AllPlatform:
		logger.ShowInfo("no platform specified, attempting to infer")
	case types.Mcdr:
		dir = serverInfo.Environments.Mcdr.PluginDirectories[0] // TODO: Change this
	case types.Forge, types.Fabric:
		// TODO: This is a temporary solution, we should have a more robust way to determine the mod directory
		dir = serverInfo.ModPath[0]
	default:
		return errors.New("unsupported platform")
	}

	p := types.Package{
		Id:           id,
		Dependencies: nil,
		Local:        nil,
		Remote:       nil,
		Supports:     nil,
		Information:  nil,
	}

	// fetch remote data
	var remoteData remote.RawPackageRemote
	var src remote.SourceHandler
	var err error

	switch cmd.String("source") {
	case "none":
		for _, src = range source.All {
			remoteData, err = src.Fetch(id)
			if err != nil {
				logger.ShowInfo(err)
				err = nil // prevent error got printed twice in the last iteration
				continue
			}
			if remoteData != nil {
				// found the package, exit loop
				break
			}
		}
	case source.Mcdr.Name().String():
		if id.Platform != types.Mcdr && id.Platform != types.AllPlatform {
			return fmt.Errorf("source 'mcdr' only supports mcdr platform")
		}
		remoteData, err = source.Mcdr.Fetch(id)
	case source.Modrinth.Name().String():
		if id.Platform == types.Mcdr {
			return fmt.Errorf("source 'modrinth' does not support mcdr platform")
		}
		remoteData, err = source.Modrinth.Fetch(id)
	default:
		return fmt.Errorf("unknown source: %s", cmd.String("source"))
	}
	if err != nil {
		logger.ReportWarn(err)
	}
	if remoteData != nil {
		r := remoteData.ToPackageRemote()
		p.Remote = &r
	} else {
		return errors.New("package not found in any source")
	}

	// let's try to get the correct dependency info first
	// for sources like modrinth, the dependency info from remote is not reliable
	if id.Platform == types.Mcdr {
		depsData, err := src.Dependencies(id)
		if err != nil {
			logger.Debug(err)
		}
		tools.PrintAsJson(depsData.ToPackageDependencies())
		return nil
	}

	// TODO: util.DownloadFile is a temporary solution
	_, _, err = util.DownloadFile(p.Remote.FileUrl, dir)
	if err != nil {
		logger.ReportError(fmt.Errorf("download failed: %w", err))
	}
	return nil
}
