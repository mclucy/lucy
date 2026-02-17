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
) (status *tui.Data) {
	longOutput := cmd.Bool("long")
	noStyle := cmd.Bool("no-style")

	if data.Executable == nil {
		return &tui.Data{
			Fields: []tui.Field{
				&tui.FieldAnnotation{
					Annotation: "(No server found)",
				},
			},
		}
	}

	status = &tui.Data{
		Fields: []tui.Field{},
	}

	status.Fields = append(
		status.Fields, &tui.FieldAnnotatedShortText{
			Title:      "Game",
			Text:       data.Executable.GameVersion.String(),
			Annotation: data.Executable.Path,
		},
	)

	if data.Executable.ModLoader != types.Minecraft {
		status.Fields = append(
			status.Fields, &tui.FieldAnnotatedShortText{
				Title:      "Modding",
				Text:       data.Executable.ModLoader.Title(),
				Annotation: data.Executable.LoaderVersion.String(),
			},
		)
	}

	if data.Activity != nil {
		status.Fields = append(
			status.Fields, &tui.FieldAnnotatedShortText{
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
		status.Fields = append(
			status.Fields, &tui.FieldShortText{
				Title: "Activity",
				Text:  tools.Dim("(Unknown)"),
			},
		)
	}

	// Modding related fields only shown when modding platform detected
	if data.Executable.ModLoader != types.Vanilla {
		if len(data.Packages) > 0 {
			modNames := make([]string, 0, len(data.Packages))
			modPaths := make([]string, 0, len(modNames))
			for _, mod := range data.Packages {
				if mod.Id.Platform == types.Forge || mod.Id.Platform == types.Fabric {
					if longOutput {
						modNames = append(modNames, mod.Id.StringFull())
					} else {
						modNames = append(modNames, mod.Id.Name.String())
					}
					modPaths = append(modPaths, mod.Local.Path)
				}
			}
			status.Fields = append(
				status.Fields,
				tools.Ternary[tui.Field](
					longOutput,
					&tui.FieldMultiAnnotatedShortText{
						Title:       "Mods",
						Texts:       modNames,
						Annotations: modPaths,
						ShowTotal:   true,
					},
					&tui.FieldDynamicColumnLabels{
						Title:     "Mods",
						Labels:    modNames,
						MaxLines:  0,
						ShowTotal: true,
					},
				),
			)
		} else {
			status.Fields = append(
				status.Fields, &tui.FieldMultiAnnotatedShortText{
					Title:       "Mods",
					Texts:       []string{tools.Dim("(None)")},
					Annotations: nil,
					ShowTotal:   false,
				},
			)
		}
	}

	// List MCDR plugins if MCDR environment detected
	if data.Environments.Mcdr != nil {
		// Tell users that MCDR is installed
		status.Fields = append(
			status.Fields, &tui.FieldShortText{
				Title: "MCDR",
				Text: "Installed" + tools.Ternary(
					noStyle,
					"",
					tools.Green(" ✓"),
				),
			},
		)
		var mcdrPlugins []string
		for _, pkg := range data.Packages {
			if pkg.Id.Platform == types.Mcdr {
				if longOutput {
					mcdrPlugins = append(mcdrPlugins, pkg.Id.StringFull())
				} else {
					mcdrPlugins = append(mcdrPlugins, pkg.Id.Name.String())
				}
			}
		}
		status.Fields = append(
			status.Fields, &tui.FieldDynamicColumnLabels{
				Title: tools.Ternary(
					noStyle,
					"MCDR Plugins",
					"└── Plugins",
				),
				Labels:    mcdrPlugins,
				MaxLines:  0,
				ShowTotal: true,
			},
		)
	}

	return status
}
