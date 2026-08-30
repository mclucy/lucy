package status

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/tui"
	"github.com/mclucy/lucy/tui/style"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Display basic information of the current server",
	Args:  cobra.NoArgs,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		logo, _ := cmd.Flags().GetString(cli.FlagLogo)
		if _, ok := tui.ParseStatusLogoMode(logo); !ok {
			return fmt.Errorf("--logo must be one of none, small, large")
		}
		return nil
	},
	RunE: cli.WithErrorLogging(actionStatus),
}

// NewCommand wires and returns the `lucy status` command.
func NewCommand() *cobra.Command {
	cli.AddJSONFlag(statusCmd)
	cli.AddLongFlag(statusCmd)
	cli.AddLogoFlag(statusCmd)
	_ = statusCmd.RegisterFlagCompletionFunc(
		cli.FlagLogo,
		func(cmd *cobra.Command, args []string, toComplete string) (
			[]string,
			cobra.ShellCompDirective,
		) {
			candidates := cli.FilterByPrefix(cli.StaticStatusLogoCandidates(), toComplete)
			return cli.ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
		},
	)
	return statusCmd
}

func actionStatus(cmd *cobra.Command, args []string) error {
	ws := workspace.New()
	json, _ := cmd.Flags().GetBool(cli.FlagJSON)
	jsonCompact, _ := cmd.Flags().GetBool(cli.FlagJSONCompact)
	long, _ := cmd.Flags().GetBool(cli.FlagLong)
	noStyle, _ := cmd.Flags().GetBool(cli.FlagNoStyle)
	if json || jsonCompact {
		if jsonCompact {
			style.PrintAsJsonCompact(ws)
		} else {
			style.PrintAsJson(ws)
		}
	} else {
		logo, _ := cmd.Flags().GetString(cli.FlagLogo)
		logoMode, _ := tui.ParseStatusLogoMode(logo)
		tui.Flush(generateStatusOutput(&ws, long, noStyle, logoMode))
	}
	return nil
}

func generateStatusOutput(
	data *workspace.Workspace,
	longOutput bool,
	noStyle bool,
	logoMode tui.StatusLogoMode,
) (output *tui.Data) {
	server := data.Server()
	hasServer := server != nil
	hasMcdr := data != nil && data.Environments.Mcdr != nil
	if !hasServer && !hasMcdr {
		return &tui.Data{
			Fields: []tui.Field{
				&tui.FieldAnnotation{
					Annotation: "(No server found)",
				},
			},
		}
	}

	output = &tui.Data{Fields: []tui.Field{}, LogoMode: logoMode}
	var effectiveEcosystems []workspace.EffectiveEcosystem
	var runtimeComponents []types.VersionedPackageRef
	if hasServer {
		effectiveEcosystems = data.EffectiveEcosystems()
		runtimeComponents = server.RuntimeComponents
	}
	modPlatforms := statusModEcosystems(effectiveEcosystems)
	showPlatformQualifiedMods := len(modPlatforms) > 1
	packageNameOutput := func(pkg types.DiscoveredPackage) string {
		if longOutput {
			return pkg.Id.StringFull()
		}
		if showPlatformQualifiedMods {
			return pkg.Id.StringBase()
		}
		return pkg.Id.Name.String()
	}
	showMods := len(modPlatforms) > 0
	showPlugins := statusHasDirectOffer(
		effectiveEcosystems,
		types.EcoBukkit,
	)
	modNames, modPaths, pluginNames, mcdrPlugins := statusPackageSections(
		data.Packages,
		runtimeComponents,
		modPlatforms,
		packageNameOutput,
		showMods,
		showPlugins,
		hasMcdr,
	)

	var logoEco types.Ecosystem
	for _, offer := range effectiveEcosystems {
		if offer.Compatibility == types.CompatFull && offer.Ecosystem.IsModding() {
			logoEco = offer.Ecosystem
			break
		}
	}
	if hasServer && logoEco == types.EcoUnspecified &&
		server.PrimaryRuntime.Eco == types.EcoMinecraft {
		logoEco = types.EcoMinecraft
	}
	if logoEco == types.EcoUnspecified && hasMcdr {
		logoEco = types.EcoMcdr
	}

	logoCore := statusLogoCore(server, hasServer, hasMcdr)
	logoVersion := statusLogoVersion(server, hasServer, hasMcdr)
	if logoMode != tui.StatusLogoNone &&
		logoCore != "" &&
		tui.GetLogo(logoCore, logoEco, logoVersion, tui.LogoSmallPlain) != "" {
		output.Fields = append(
			output.Fields,
			&tui.FieldLogo{
				Core:    logoCore,
				Eco:     logoEco,
				Version: logoVersion,
				NoColor: noStyle,
				Mode:    logoMode,
			},
		)
	}

	if hasServer {
		output.Fields = append(
			output.Fields,
			&tui.FieldAnnotatedShortText{
				Title:      "Game",
				Text:       server.GameVersion().String(),
				Annotation: server.PrimaryPath,
			},
		)

		output.Fields = append(
			output.Fields, &tui.FieldShortText{
				Title: "Activity",
				Text: fn.Ternary(
					data.Active(),
					"Active",
					"Inactive",
				),
			},
		)

		primary := server.PrimaryRuntime
		if platformLabel := statusRuntimeLabel(primary); platformLabel != "" {
			children := make([]tui.TreeNode, 0, 4)
			if len(server.RuntimeComponents) > 0 {
				components := make(
					[]string,
					0,
					len(server.RuntimeComponents),
				)
				for _, component := range server.RuntimeComponents {
					components = append(components, component.StringFull())
				}
				children = append(children, tui.TreeNode{
					Title: "Components",
					Field: statusPackageListField(components, nil, false),
				})
			}
			if len(effectiveEcosystems) > 0 {
				offers := make([]string, 0, len(effectiveEcosystems))
				for _, offer := range effectiveEcosystems {
					offers = append(
						offers,
						fmt.Sprintf(
							"%s (%s)",
							offer.Ecosystem.Title(),
							offer.Compatibility,
						),
					)
				}
				children = append(children, tui.TreeNode{
					Title: "Offers",
					Field: statusPackageListField(offers, nil, false),
				})
			}
			if showMods {
				children = append(
					children,
					tui.TreeNode{
						Title: "Mods",
						Field: statusPackageListField(
							modNames,
							modPaths,
							longOutput,
						),
					},
				)
			}
			if showPlugins {
				children = append(
					children,
					tui.TreeNode{
						Title: "Plugins",
						Field: statusPackageListField(pluginNames, nil, false),
					},
				)
			}
			output.Fields = append(
				output.Fields, &tui.FieldTree{
					Title:      "Platform",
					Text:       platformLabel,
					Annotation: primary.Version.String(),
					Children:   children,
				},
			)
		}
	}

	if hasMcdr {
		children := []tui.TreeNode{
			{
				Title: "Plugins",
				Field: statusPackageListField(mcdrPlugins, nil, false),
			},
		}
		output.Fields = append(
			output.Fields, &tui.FieldTree{
				Title: "MCDR",
				Text: "Installed" + fn.Ternary(
					noStyle,
					"",
					style.Success(" ✓"),
				),
				Children: children,
			},
		)
	}

	return output
}

func statusLogoCore(
	server *workspace.ServerInstance,
	hasServer bool,
	hasMcdr bool,
) types.BarePackageName {
	if hasServer && server != nil {
		return server.PrimaryRuntime.Name
	}
	if hasMcdr {
		return "mcdr"
	}
	return ""
}

func statusLogoVersion(
	server *workspace.ServerInstance,
	hasServer bool,
	hasMcdr bool,
) types.BareVersion {
	if hasServer && server != nil {
		return server.GameVersion()
	}
	if hasMcdr {
		return types.VersionUnknown
	}
	return ""
}

func statusPackageListField(
	names []string,
	paths []string,
	longOutput bool,
) tui.Field {
	if len(names) == 0 {
		return &tui.FieldShortText{Text: style.Muted("(None)")}
	}
	if longOutput {
		return &tui.FieldMultiAnnotatedShortText{
			Texts:       names,
			Annotations: paths,
			ShowTotal:   true,
		}
	}
	return &tui.FieldDynamicColumnLabels{
		Labels:    names,
		MaxLines:  0,
		ShowTotal: true,
	}
}

func statusPackageSections(
	packages []types.DiscoveredPackage,
	runtimeComponents []types.VersionedPackageRef,
	modPlatforms map[types.Ecosystem]bool,
	packageNameOutput func(types.DiscoveredPackage) string,
	showMods bool,
	showPlugins bool,
	hasMcdr bool,
) ([]string, []string, []string, []string) {
	componentKeys := make(map[string]struct{}, len(runtimeComponents))
	for _, component := range runtimeComponents {
		componentKeys[component.StringBase()] = struct{}{}
	}

	modNames := make([]string, 0, len(packages))
	modPaths := make([]string, 0, len(packages))
	pluginNames := make([]string, 0, len(packages))
	mcdrPlugins := make([]string, 0, len(packages))
	for _, pkg := range packages {
		if _, isComponent := componentKeys[pkg.Id.StringBase()]; isComponent {
			continue
		}
		if types.IsCorePackage(pkg.Id.PackageRef) {
			continue
		}

		packagePlatform := pkg.Id.Eco
		if showMods && modPlatforms[packagePlatform] {
			modNames = append(modNames, packageNameOutput(pkg))
			if pkg.Path != "" {
				modPaths = append(modPaths, pkg.Path)
			}
		}
		if showPlugins &&
			(packagePlatform == types.EcoBukkit ||
				packagePlatform == types.EcoPaper) {
			pluginNames = append(pluginNames, packageNameOutput(pkg))
		}
		if hasMcdr && packagePlatform == types.EcoMcdr {
			mcdrPlugins = append(mcdrPlugins, packageNameOutput(pkg))
		}
	}

	return modNames, modPaths, pluginNames, mcdrPlugins
}

func statusModEcosystems(
	offers []workspace.EffectiveEcosystem,
) map[types.Ecosystem]bool {
	platforms := make(map[types.Ecosystem]bool, 3)
	for _, offer := range offers {
		if offer.Ecosystem.IsModding() {
			platforms[offer.Ecosystem] = true
		}
	}
	return platforms
}

func statusHasDirectOffer(
	offers []workspace.EffectiveEcosystem,
	required types.Ecosystem,
) bool {
	for _, offer := range offers {
		if offer.Compatibility == types.CompatFull &&
			offer.Ecosystem.Satisfy(required) {
			return true
		}
	}
	return false
}

func statusRuntimeLabel(primary *types.VersionedPackageRef) string {
	if primary == nil {
		return ""
	}
	switch strings.ToLower(primary.Name.String()) {
	case "minecraft":
		return "Vanilla"
	case "mcdreforged":
		return "MCDReforged"
	case "neoforge":
		return "NeoForge"
	case "craftbukkit":
		return "CraftBukkit"
	case "catserver":
		return "CatServer"
	case "bungeecord":
		return "BungeeCord"
	case "spongevanilla":
		return "SpongeVanilla"
	case "spongeforge":
		return "SpongeForge"
	case "spongeneo":
		return "SpongeNeo"
	default:
		return style.Capitalize(strings.ReplaceAll(
			strings.ReplaceAll(primary.Name.String(), "-", " "),
			"_",
			" ",
		))
	}
}
