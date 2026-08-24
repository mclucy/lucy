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
	RunE:  cli.WithErrorLogging(actionStatus),
}

// NewCommand wires and returns the `lucy status` command.
func NewCommand() *cobra.Command {
	cli.AddJSONFlag(statusCmd)
	cli.AddLongFlag(statusCmd)
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
		tui.Flush(generateStatusOutput(&ws, long, noStyle))
	}
	return nil
}

func generateStatusOutput(
	data *workspace.Workspace,
	longOutput bool,
	noStyle bool,
) (output *tui.Data) {
	hasServer := data != nil && data.Server != nil && data.Server.IsValid()
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

	output = &tui.Data{Fields: []tui.Field{}}
	var effectiveEcosystems []workspace.EffectiveEcosystem
	var runtimeComponents []types.VersionedPackageRef
	if hasServer {
		effectiveEcosystems = data.Server.EffectiveEcosystems()
		runtimeComponents = data.Server.RuntimeComponents
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
		if offer.Compatibility == types.CompatCompatible &&
			offer.Ecosystem.IsModding() {
			logoEco = offer.Ecosystem
			break
		}
	}
	if hasServer &&
		logoEco == types.EcoUnspecified &&
		data.Server.PrimaryRuntime.Eco == types.EcoMinecraft {
		logoEco = types.EcoMinecraft
	}
	if logoEco == types.EcoUnspecified && hasMcdr {
		logoEco = types.EcoMcdr
	}
	if logoEco != types.EcoUnspecified {
		output.Fields = append(
			output.Fields,
			&tui.FieldLogo{
				Eco:     logoEco,
				NoColor: noStyle,
			},
		)
	}

	if hasServer {
		output.Fields = append(
			output.Fields,
			&tui.FieldAnnotatedShortText{
				Title:      "Game",
				Text:       data.Server.GameVersion().String(),
				Annotation: data.Server.PrimaryPath,
			},
		)

		if data.Activity != nil {
			output.Fields = append(
				output.Fields, &tui.FieldAnnotatedShortText{
					Title: "Activity",
					Text: fn.Ternary(
						data.Activity.Active,
						"Active",
						"Inactive",
					),
					Annotation: fn.Ternary(
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
					Text:  style.Muted("(Unknown)"),
				},
			)
		}

		primary := data.Server.PrimaryRuntime
		if platformLabel := statusRuntimeLabel(primary); platformLabel != "" {
			children := make([]tui.TreeNode, 0, 4)
			if len(data.Server.RuntimeComponents) > 0 {
				components := make(
					[]string,
					0,
					len(data.Server.RuntimeComponents),
				)
				for _, component := range data.Server.RuntimeComponents {
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
		if offer.Compatibility == types.CompatCompatible &&
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
