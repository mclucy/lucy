package status

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/terminal"
	"github.com/mclucy/lucy/terminal/style"
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
		if _, ok := terminal.ParseStatusLogoMode(logo); !ok {
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

// actionStatus probes the selected workspace and renders its local runtime and
// package observations in the requested human-readable or JSON format.
func actionStatus(cmd *cobra.Command, args []string) error {
	target, err := cli.ResolveCommandTarget(cmd)
	if err != nil {
		return err
	}
	ws := workspace.NewAt(target.WorkDir)
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
		logoMode, _ := terminal.ParseStatusLogoMode(logo)
		terminal.Flush(generateStatusOutput(&ws, long, noStyle, logoMode))
	}
	return nil
}

func generateStatusOutput(
	data *workspace.Workspace,
	longOutput bool,
	noStyle bool,
	logoMode terminal.StatusLogoMode,
) (output *terminal.Data) {
	server := data.Server()
	hasServer := server != nil
	hasMcdr := data != nil && data.Environments.Mcdr != nil
	if !hasServer && !hasMcdr {
		return &terminal.Data{
			Fields: []terminal.Field{
				&terminal.FieldAnnotation{
					Annotation: "(No server found)",
				},
			},
		}
	}

	output = &terminal.Data{Fields: []terminal.Field{}, LogoMode: logoMode}
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
	if logoMode != terminal.StatusLogoNone &&
		logoCore != "" &&
		terminal.GetLogo(logoCore, logoEco, logoVersion, terminal.LogoSmallPlain) != "" {
		output.Fields = append(
			output.Fields,
			&terminal.FieldLogo{
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
			&terminal.FieldAnnotatedShortText{
				Title:      "Game",
				Text:       server.GameVersion().String(),
				Annotation: server.PrimaryPath,
			},
		)

		output.Fields = append(
			output.Fields, &terminal.FieldShortText{
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
			children := make([]terminal.TreeNode, 0, 4)
			if len(server.RuntimeComponents) > 0 {
				components := make(
					[]string,
					0,
					len(server.RuntimeComponents),
				)
				for _, component := range server.RuntimeComponents {
					components = append(components, component.StringFull())
				}
				children = append(children, terminal.TreeNode{
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
				children = append(children, terminal.TreeNode{
					Title: "Offers",
					Field: statusPackageListField(offers, nil, false),
				})
			}
			if showMods {
				children = append(
					children,
					terminal.TreeNode{
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
					terminal.TreeNode{
						Title: "Plugins",
						Field: statusPackageListField(pluginNames, nil, false),
					},
				)
			}
			output.Fields = append(
				output.Fields, &terminal.FieldTree{
					Title:      "Platform",
					Text:       platformLabel,
					Annotation: primary.Version.String(),
					Children:   children,
				},
			)
		}
	}

	if hasMcdr {
		children := []terminal.TreeNode{
			{
				Title: "Plugins",
				Field: statusPackageListField(mcdrPlugins, nil, false),
			},
		}
		output.Fields = append(
			output.Fields, &terminal.FieldTree{
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
) terminal.Field {
	if len(names) == 0 {
		return &terminal.FieldShortText{Text: style.Muted("(None)")}
	}
	if longOutput {
		return &terminal.FieldMultiAnnotatedShortText{
			Texts:       names,
			Annotations: paths,
			ShowTotal:   true,
		}
	}
	return &terminal.FieldDynamicColumnLabels{
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
		if types.IsCorePackage(types.PackageRequest{PackageRef: pkg.Id.PackageRef, Eco: pkg.Id.Eco, Version: pkg.Id.Version}) {
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
