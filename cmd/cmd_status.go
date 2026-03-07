package cmd

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/tui"
	"github.com/mclucy/lucy/types"

	"github.com/urfave/cli/v3"
)

var subcmdStatus = &cli.Command{
	Name:  "status",
	Usage: "Display basic information of the current server",
	Action: tools.Decorate(
		actionStatus,
		decoratorGlobalFlags,
	),
	Flags: []cli.Flag{
		flagJsonOutput,
		flagLongOutput,
	},
}

var actionStatus cli.ActionFunc = func(
	_ context.Context,
	cmd *cli.Command,
) error {
	serverInfo := probe.ServerInfo()
	if cmd.Bool("json") {
		tools.PrintAsJson(serverInfo)
	} else {
		tui.Flush(generateStatusOutput(&serverInfo, cmd))
	}
	return nil
}

func generateStatusOutput(
	data *types.ServerInfo,
	cmd *cli.Command,
) (output *tui.Data) {
	longOutput := cmd.Bool("long")
	noStyle := cmd.Bool("no-style")
	serverPlatform := data.Executable.ModLoader
	hasMcdr := data.Environments.Mcdr != nil
	hasLucy := data.Environments.Lucy != nil

	packageNameOutput := tools.Ternary(
		longOutput,
		func(pkg types.Package) string { return pkg.Id.StringFull() },
		func(pkg types.Package) string { return pkg.Id.Name.String() },
	)

	if data.Executable == nil {
		return &tui.Data{
			Fields: []tui.Field{
				&tui.FieldAnnotation{
					Annotation: "(No server found)",
				},
			},
		}
	}

	output = &tui.Data{Fields: []tui.Field{}}

	// logo display strategy:
	// custom client > mod loader > mcdr > lucy > vanilla
	var logoPlatform types.Platform
	if serverPlatform == types.PlatformVanilla {
		if hasMcdr {
			logoPlatform = types.PlatformMCDR
		} else if hasLucy {
			// logoPlatform =
			// lucy is not supposed to be a platform, needs refactor
			// also need structural support for all other custom server clients
		} else {
			logoPlatform = types.PlatformVanilla
		}
	} else if serverPlatform.IsModding() {
		output.Fields = append(
			output.Fields,
			&tui.FieldLogo{
				Platform: logoPlatform,
				NoColor:  noStyle,
			},
		)
	}

	output.Fields = append(
		output.Fields,
		&tui.FieldAnnotatedShortText{
			Title:      "Game",
			Text:       data.Executable.GameVersion.String(),
			Annotation: data.Executable.Path,
		},
	)

	if data.Activity != nil {
		output.Fields = append(
			output.Fields, &tui.FieldAnnotatedShortText{
				Title: "Activity",
				Text: tools.Ternary(
					data.Activity.Active,
					"Active",
					"Inactive",
				),
				Annotation: tools.Ternary(
					data.Activity.Active,
					fmt.Sprintf("PID %d", data.Activity.Pid),
					"",
				),
			},
		)
	} else {
		output.Fields = append(
			output.Fields, &tui.FieldShortText{
				Title: "Activity",
				Text:  tools.Dim("(Unknown)"),
			},
		)
	}

	// Show modding platform if detected, even if no mods found, to differentiate
	// between modded and vanilla servers
	if data.Executable.ModLoader != types.PlatformMinecraft {
		output.Fields = append(
			output.Fields, &tui.FieldAnnotatedShortText{
				Title:      "Platform",
				Text:       data.Executable.ModLoader.Title(),
				Annotation: data.Executable.LoaderVersion.String(),
			},
		)
	}

	showMods := data.Executable.ModLoader.IsModding()

	// Collect mod/plugin names and paths for later use. This is to avoid
	// traversing the package list multiple times, which can be costly when
	// there are many packages.
	var modNames []string
	var modPaths []string
	var mcdrPlugins []string
	if showMods {
		modNames = make([]string, 0, len(data.Packages))
		modPaths = make([]string, 0, len(data.Packages))
	}
	if hasMcdr {
		mcdrPlugins = make([]string, 0, len(data.Packages))
	}
	if showMods || hasMcdr {
		for _, p := range data.Packages {
			packagePlatform := p.Id.Platform
			if showMods && packagePlatform == serverPlatform {
				modNames = append(modNames, packageNameOutput(p))
				modPaths = append(modPaths, p.Local.Path)
			}
			if hasMcdr && packagePlatform == types.PlatformMCDR {
				mcdrPlugins = append(mcdrPlugins, packageNameOutput(p))
			}
		}
	}

	// Modding related fields only shown when modding platform detected
	if showMods {
		modListTitle := tools.Ternary(
			noStyle,
			"Mods",
			"└── Mods",
		)
		if len(modNames) == 0 {
			output.Fields = append(
				output.Fields, &tui.FieldShortText{
					Title: modListTitle,
					Text:  tools.Dim("(None)"),
				},
			)
		} else {
			output.Fields = append(
				output.Fields,
				tools.Ternary[tui.Field](
					longOutput,
					&tui.FieldMultiAnnotatedShortText{
						Title:       modListTitle,
						Texts:       modNames,
						Annotations: modPaths,
						ShowTotal:   true,
					},
					&tui.FieldDynamicColumnLabels{
						Title:     modListTitle,
						Labels:    modNames,
						MaxLines:  0,
						ShowTotal: true,
					},
				),
			)
		}
	}

	// List MCDR plugins if MCDR environment detected
	if hasMcdr {
		mcdrPluginListTitle := tools.Ternary(
			noStyle,
			"MCDR Plugins",
			"└── Plugins",
		)

		// Tell users that MCDR is installed
		output.Fields = append(
			output.Fields, &tui.FieldShortText{
				Title: "MCDR",
				Text: "Installed" + tools.Ternary(
					noStyle,
					"",
					tools.Green(" ✓"),
				),
			},
		)

		if len(mcdrPlugins) == 0 {
			output.Fields = append(
				output.Fields, &tui.FieldShortText{
					Title: mcdrPluginListTitle,
					Text:  tools.Dim("(None)"),
				},
			)
		} else {
			output.Fields = append(
				output.Fields, &tui.FieldDynamicColumnLabels{
					Title:     mcdrPluginListTitle,
					Labels:    mcdrPlugins,
					MaxLines:  0,
					ShowTotal: true,
				},
			)
		}
	}

	return output
}
