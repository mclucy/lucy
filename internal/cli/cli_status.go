package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/terminal"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"

	"github.com/spf13/cobra"
)

const flagStatusFromLockName = "from-lock"

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: help.StatusShort,
	Args:  cobra.NoArgs,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		logo, _ := cmd.Flags().GetString(FlagLogo)
		if _, ok := terminal.ParseStatusLogoMode(logo); !ok {
			return fmt.Errorf("--logo must be one of none, small, large")
		}
		return nil
	},
	RunE: actionStatus,
}

func NewStatusCommand() *cobra.Command {
	AddLongFlag(statusCmd)
	AddLogoFlag(statusCmd)
	statusCmd.Flags().Bool(
		flagStatusFromLockName,
		false,
		"Render the server identity and packages from lucy-lock.yaml instead of the live directory",
	)
	_ = statusCmd.RegisterFlagCompletionFunc(
		FlagLogo,
		func(cmd *cobra.Command, args []string, toComplete string) (
			[]string,
			cobra.ShellCompDirective,
		) {
			candidates := FilterByPrefix(StaticStatusLogoCandidates(), toComplete)
			return ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
		},
	)
	return statusCmd
}

func actionStatus(cmd *cobra.Command, args []string) error {
	target, err := ResolveCommandTarget(cmd)
	if err != nil {
		return err
	}

	long, _ := cmd.Flags().GetBool(FlagLong)
	logo, _ := cmd.Flags().GetString(FlagLogo)
	logoMode, _ := terminal.ParseStatusLogoMode(logo)

	fromLock, _ := cmd.Flags().GetBool(flagStatusFromLockName)
	if fromLock {
		return actionStatusFromLock(cmd, target.WorkDir, long, logoMode)
	}

	ws := workspace.NewAt(target.WorkDir)
	reportObservationFindings(ws)
	switch JSONMode(cmd) {
	case JSONPretty:
		style.PrintAsJson(ws)
		return nil
	case JSONCompact:
		style.PrintAsJsonCompact(ws)
		return nil
	}
	terminal.Flush(
		generateStatusOutput(&ws, long, style.ColorsEnabled(), logoMode),
	)
	return nil
}

func generateStatusOutput(
	data *workspace.Workspace,
	longOutput bool,
	colorsEnabled bool,
	logoMode terminal.StatusLogoMode,
) (output *terminal.Data) {
	server := data.Runtime()
	hasServer := server != nil
	hasMcdr := data != nil && data.Wrappers.MCDR != nil
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
	var servedEcosystems []workspace.ServedEcosystem
	var runtimeComponents []types.VersionedPackageRef
	if hasServer {
		servedEcosystems = data.ServedEcosystems()
		runtimeComponents = server.Components
	}
	modEcosystems := statusModEcosystems(servedEcosystems)
	showEcosystemQualifiedMods := len(modEcosystems) > 1
	packageNameOutput := func(pkg types.DiscoveredPackage) string {
		if longOutput {
			return pkg.ID.StringBase()
		}
		if showEcosystemQualifiedMods {
			return pkg.ID.StringBase()
		}
		return pkg.ID.Name.String()
	}
	showMods := len(modEcosystems) > 0
	showPlugins := statusHasDirectOffer(
		servedEcosystems,
		types.EcosystemBukkit,
	)
	modNames, modPaths, pluginNames, mcdrPlugins := statusPackageSections(
		data.Packages,
		runtimeComponents,
		modEcosystems,
		packageNameOutput,
		showMods,
		showPlugins,
		hasMcdr,
	)

	var logoEcosystem types.Ecosystem
	for _, offer := range servedEcosystems {
		if offer.Compatibility == types.CompatFull && offer.Ecosystem.IsModding() {
			logoEcosystem = offer.Ecosystem
			break
		}
	}
	if hasServer && logoEcosystem == types.EcosystemUnspecified &&
		server.Primary != nil &&
		server.Primary.Ecosystem == types.EcosystemMinecraft {
		logoEcosystem = types.EcosystemMinecraft
	}
	if logoEcosystem == types.EcosystemUnspecified && hasMcdr {
		logoEcosystem = types.EcosystemMCDR
	}

	logoProduct := statusLogoProduct(server, hasServer, hasMcdr)
	logoVersion := statusLogoVersion(server, hasServer, hasMcdr)
	if logoMode != terminal.StatusLogoNone &&
		logoProduct != types.ServerProductUnknown &&
		terminal.GetLogo(logoProduct, logoEcosystem, logoVersion, terminal.LogoSmallPlain) != "" {
		output.Fields = append(
			output.Fields,
			&terminal.FieldLogo{
				Product:   logoProduct,
				Ecosystem: logoEcosystem,
				Version:   logoVersion,
				NoColor:   !colorsEnabled,
				Mode:      logoMode,
			},
		)
	}

	if hasServer {
		output.Fields = append(
			output.Fields,
			&terminal.FieldAnnotatedShortText{
				Title:      "Minecraft",
				Text:       server.MinecraftVersion().String(),
				Annotation: server.PrimaryPath,
			},
		)

		output.Fields = append(
			output.Fields, &terminal.FieldShortText{
				Title: "Activity",
				Text: fn.Ternary(
					data.Running(),
					"Active",
					"Inactive",
				),
			},
		)

		primary := server.Primary
		if productLabel := statusRuntimeLabel(primary); productLabel != "" {
			children := make([]terminal.TreeNode, 0, 4)
			if len(server.Components) > 0 {
				components := make(
					[]string,
					0,
					len(server.Components),
				)
				for _, component := range server.Components {
					components = append(components, component.StringQualified())
				}
				children = append(children, terminal.TreeNode{
					Title: "Runtime components",
					Field: statusPackageListField(components, nil, false),
				})
			}
			if len(servedEcosystems) > 0 {
				offers := make([]string, 0, len(servedEcosystems))
				for _, offer := range servedEcosystems {
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
					Title: "Compatible ecosystems",
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
					Title:      "Server product",
					Text:       productLabel,
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
				Title:    "MCDR",
				Text:     "Installed" + style.Success(" ✓"),
				Children: children,
			},
		)
	}

	return output
}

func statusLogoProduct(
	server *workspace.ServerRuntime,
	hasServer bool,
	hasMcdr bool,
) types.ServerProduct {
	if hasServer && server != nil {
		return server.Product()
	}
	if hasMcdr {
		return types.ServerProductMCDReforged
	}
	return types.ServerProductUnknown
}

func statusLogoVersion(
	server *workspace.ServerRuntime,
	hasServer bool,
	hasMcdr bool,
) types.Version {
	if hasServer && server != nil {
		return server.MinecraftVersion()
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
	modEcosystems map[types.Ecosystem]bool,
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
		if _, isComponent := componentKeys[pkg.ID.StringBase()]; isComponent {
			continue
		}
		if types.IsServerProduct(types.VersionedPackageRef{
			PackageRef: pkg.ID.PackageRef,
			Ecosystem:  pkg.ID.Ecosystem,
			Version:    pkg.ID.Version,
		}) {
			continue
		}

		packageEcosystem := pkg.ID.Ecosystem
		if showMods && modEcosystems[packageEcosystem] {
			modNames = append(modNames, packageNameOutput(pkg))
			if pkg.Path != "" {
				modPaths = append(modPaths, pkg.Path)
			}
		}
		if showPlugins &&
			(packageEcosystem == types.EcosystemBukkit ||
				packageEcosystem == types.EcosystemPaper) {
			pluginNames = append(pluginNames, packageNameOutput(pkg))
		}
		if hasMcdr && packageEcosystem == types.EcosystemMCDR {
			mcdrPlugins = append(mcdrPlugins, packageNameOutput(pkg))
		}
	}

	return modNames, modPaths, pluginNames, mcdrPlugins
}

func statusModEcosystems(
	offers []workspace.ServedEcosystem,
) map[types.Ecosystem]bool {
	ecosystems := make(map[types.Ecosystem]bool, 3)
	for _, offer := range offers {
		if offer.Ecosystem.IsModding() {
			ecosystems[offer.Ecosystem] = true
		}
	}
	return ecosystems
}

func statusHasDirectOffer(
	offers []workspace.ServedEcosystem,
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

// lockStatusDocument is the `status --from-lock` wire format. It reports the
// exact facts recorded in lucy-lock.yaml instead of observing the directory.
type lockStatusDocument struct {
	Root     string                  `json:"root"`
	Server   lockStatusServer        `json:"server"`
	MCDR     *lockStatusMCDR         `json:"mcdr,omitempty"`
	Packages []lockStatusPackageView `json:"packages"`
}

type lockStatusServer struct {
	Product      types.ServerProduct `json:"product"`
	Minecraft    types.Version       `json:"minecraft"`
	CoreVersion  types.Version       `json:"core_version"`
	Ecosystem    types.Ecosystem     `json:"ecosystem"`
	Directory    string              `json:"directory"`
	Bootstrapper string              `json:"bootstrapper,omitempty"`
}

type lockStatusMCDR struct {
	Directory string        `json:"directory"`
	Version   types.Version `json:"version"`
	Python    string        `json:"python"`
	Target    string        `json:"target"`
}

type lockStatusPackageView struct {
	ID          string            `json:"id"`
	Name        types.PackageName `json:"name"`
	Provider    types.Provider    `json:"provider"`
	ProjectID   string            `json:"project_id"`
	Ecosystem   types.Ecosystem   `json:"ecosystem"`
	Environment types.Environment `json:"environment"`
	Version     types.Version     `json:"version"`
	Filename    string            `json:"filename"`
}

func actionStatusFromLock(
	cmd *cobra.Command,
	workDir string,
	longOutput bool,
	logoMode terminal.StatusLogoMode,
) error {
	lock, err := lockfile.Read(workDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf(
				"no %s to render; run lucy install first",
				lockfile.Filename,
			)
		}
		return fmt.Errorf("read %s: %w", lockfile.Filename, err)
	}

	// The manifest declares the server identity; the lock records the
	// resolved facts. Reading the manifest is best-effort because a lock can
	// outlive its manifest, and the lock alone still describes the installed
	// state.
	declared, _ := manifest.Read(workDir)
	document := newLockStatusDocument(workDir, lock, declared)
	switch JSONMode(cmd) {
	case JSONPretty:
		style.PrintAsJson(document)
		return nil
	case JSONCompact:
		style.PrintAsJsonCompact(document)
		return nil
	}
	terminal.Flush(
		generateLockStatusOutput(
			document,
			longOutput,
			style.ColorsEnabled(),
			logoMode,
		),
	)
	return nil
}

func newLockStatusDocument(
	workDir string,
	lock *lockfile.Document,
	declared *manifest.Document,
) lockStatusDocument {
	document := lockStatusDocument{
		Root: workDir,
		Server: lockStatusServer{
			Product:      lock.Server.Product,
			Minecraft:    lock.Server.Minecraft,
			CoreVersion:  lock.Server.CoreVersion,
			Ecosystem:    ecosystemFromLock(lock),
			Directory:    lock.Server.Directory,
			Bootstrapper: lock.Server.Bootstrapper,
		},
		Packages: make([]lockStatusPackageView, 0, len(lock.Packages)),
	}
	if declared != nil {
		// The resolved lock wins on exact values; the manifest identifies
		// the server that the lock was generated for.
		if product, ok := types.ParseServerProduct(
			declared.Server.Core.Product.String(),
		); ok {
			document.Server.Product = product
		}
		if declared.Server.Minecraft != "" {
			document.Server.Minecraft = declared.Server.Minecraft
		}
		if declared.Server.Directory != "" {
			document.Server.Directory = declared.Server.Directory
		}
	}
	if lock.MCDR != nil {
		document.MCDR = &lockStatusMCDR{
			Directory: lock.MCDR.Directory,
			Version:   lock.MCDR.Version,
			Python:    lock.MCDR.Python,
			Target:    lock.MCDR.Target,
		}
	}
	for _, pkg := range lock.Packages {
		document.Packages = append(
			document.Packages,
			lockStatusPackageView{
				ID:          pkg.Provider.String() + ":" + pkg.ProjectID,
				Name:        types.PackageName(pkg.ProjectID),
				Provider:    pkg.Provider,
				ProjectID:   pkg.ProjectID,
				Ecosystem:   pkg.Ecosystem,
				Environment: pkg.Environment,
				Version:     pkg.Version,
				Filename:    pkg.Filename,
			},
		)
	}
	return document
}

func generateLockStatusOutput(
	document lockStatusDocument,
	longOutput bool,
	colorsEnabled bool,
	logoMode terminal.StatusLogoMode,
) *terminal.Data {
	output := &terminal.Data{Fields: []terminal.Field{}, LogoMode: logoMode}

	if logoMode != terminal.StatusLogoNone &&
		document.Server.Product != types.ServerProductUnknown &&
		terminal.GetLogo(
			document.Server.Product,
			document.Server.Ecosystem,
			document.Server.Minecraft,
			terminal.LogoSmallPlain,
		) != "" {
		output.Fields = append(
			output.Fields,
			&terminal.FieldLogo{
				Product:   document.Server.Product,
				Ecosystem: document.Server.Ecosystem,
				Version:   document.Server.Minecraft,
				NoColor:   !colorsEnabled,
				Mode:      logoMode,
			},
		)
	}

	output.Fields = append(
		output.Fields,
		&terminal.FieldAnnotatedShortText{
			Title:      "Minecraft",
			Text:       document.Server.Minecraft.String(),
			Annotation: document.Server.Directory,
		},
	)

	serverNames := make([]string, 0, len(document.Packages))
	mcdrNames := make([]string, 0, len(document.Packages))
	for _, pkg := range document.Packages {
		label := fmt.Sprintf(
			"%s@%s",
			pkg.ID,
			pkg.Version.String(),
		)
		if longOutput {
			label += " (" + pkg.Filename + ")"
		}
		if pkg.Environment == types.EnvironmentMCDR {
			mcdrNames = append(mcdrNames, label)
			continue
		}
		serverNames = append(serverNames, label)
	}

	serverChildren := []terminal.TreeNode{
		{
			Title: "Packages",
			Field: statusPackageListField(serverNames, nil, false),
		},
	}
	output.Fields = append(
		output.Fields,
		&terminal.FieldTree{
			Title:      "Server product",
			Text:       document.Server.Product.Title(),
			Annotation: document.Server.CoreVersion.String(),
			Children:   serverChildren,
		},
	)

	if document.MCDR != nil {
		version := document.MCDR.Version.String()
		if longOutput {
			version += " (" + document.MCDR.Python + ")"
		}
		output.Fields = append(
			output.Fields,
			&terminal.FieldTree{
				Title: "MCDR",
				Text:  version,
				Children: []terminal.TreeNode{
					{
						Title: "Plugins",
						Field: statusPackageListField(mcdrNames, nil, false),
					},
				},
			},
		)
	}

	return output
}
