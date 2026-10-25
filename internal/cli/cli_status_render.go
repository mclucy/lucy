package cli

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mclucy/lucy/catalog"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/terminal"
	"github.com/mclucy/lucy/terminal/style"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
)

const (
	// statusViewPackageGridLines bounds the default package grid. The total
	// package count stays visible, and the dim tail marker reports the rest.
	statusViewPackageGridLines = 6
	statusViewColumnGap        = 2
)

// renderStatus renders the human status view from the same document that
// produces JSON. The caller supplies the width; process globals only decide
// whether the logo may appear at all.
func renderStatus(
	document statusDocument,
	long bool,
	logoMode terminal.StatusLogoMode,
	width int,
) string {
	if width <= 0 {
		width = 80
	}

	logo := resolveStatusViewLogo(document)
	layout := terminal.NegotiateStatusLayout(
		width,
		logo.largeWidth(),
		logo.smallWidth(),
		style.StdoutIsTerminal,
		logoMode,
	)
	if logo.empty() {
		layout = terminal.StatusLayoutParams{
			Mode:      terminal.LayoutInfoOnly,
			InfoWidth: width,
		}
	}
	if layout.InfoWidth <= 0 {
		layout.InfoWidth = width
	}

	infoWidth := max(1, layout.InfoWidth-2)
	packages := statusViewPackageGroups(document.Packages)
	facts := statusViewFacts(document, packages, long, infoWidth)
	info := terminal.RenderFactGroups(facts, infoWidth, style.Key)
	if long {
		for index, group := range packages {
			separator := "\n\n"
			if index == 0 && len(facts) > 1 && len(facts[1]) > 0 {
				separator = "\n"
			}
			info += separator + group.title + " " + style.Muted(fmt.Sprintf("(%d)", len(group.packages)))
			info += "\n" + statusViewPackageTable(document.Root, group.packages, infoWidth)
		}
	}
	info = "  " + strings.ReplaceAll(info, "\n", "\n  ")
	return statusViewCompose(info, logo, layout, logoMode)
}

type statusViewPackageGroup struct {
	title    string
	packages []statusPackage
}

type statusViewLogo struct {
	product   serverpkg.Product
	ecosystem catalog.Ecosystem
	version   versionpkg.Version
	large     []string
	small     []string
}

func (l statusViewLogo) empty() bool {
	return len(l.large) == 0 && len(l.small) == 0
}

func (l statusViewLogo) largeWidth() int {
	return statusViewBlockWidth(l.large)
}

func (l statusViewLogo) smallWidth() int {
	return statusViewBlockWidth(l.small)
}

func statusViewBlockWidth(lines []string) int {
	if len(lines) == 0 {
		return 0
	}
	return lipgloss.Width(lines[0])
}

func resolveStatusViewLogo(document statusDocument) statusViewLogo {
	if document.Server != nil {
		product, ok := serverpkg.ParseProduct(document.Server.Product.Name)
		if ok {
			logo := statusViewLogo{
				product:   product,
				ecosystem: statusViewLogoEcosystem(document.Server),
				version:   versionpkg.Version(document.Server.Minecraft),
			}
			if terminal.GetLogo(
				logo.product,
				logo.ecosystem,
				logo.version,
				terminal.LogoSmallPlain,
			) == "" {
				fallback := catalog.Ecosystem(
					strings.ToLower(document.Server.Product.Ecosystem),
				)
				if fallback.Valid() &&
					fallback != catalog.EcosystemUnspecified &&
					terminal.GetLogo(
						logo.product,
						fallback,
						logo.version,
						terminal.LogoSmallPlain,
					) != "" {
					logo.ecosystem = fallback
				}
			}
			return statusViewLogoAssets(logo)
		}
	}
	if document.MCDR != nil {
		return statusViewLogoAssets(statusViewLogo{
			product:   serverpkg.ProductMCDReforged,
			ecosystem: catalog.EcosystemMCDR,
			version:   versionpkg.Unknown,
		})
	}
	return statusViewLogo{}
}

// A full modding ecosystem selects the logo before the product's own ecosystem.
func statusViewLogoEcosystem(server *statusServer) catalog.Ecosystem {
	var fallback catalog.Ecosystem
	for _, offer := range server.CompatibleEcosystems {
		ecosystem := catalog.Ecosystem(strings.ToLower(offer.Ecosystem))
		if !ecosystem.Valid() || ecosystem == catalog.EcosystemUnspecified {
			continue
		}
		if offer.Compatibility == string(workspace.CompatFull) &&
			ecosystem.IsModding() {
			return ecosystem
		}
		if fallback == catalog.EcosystemUnspecified {
			fallback = ecosystem
		}
	}
	productEcosystem := catalog.Ecosystem(
		strings.ToLower(server.Product.Ecosystem),
	)
	if productEcosystem.Valid() &&
		productEcosystem != catalog.EcosystemUnspecified {
		return productEcosystem
	}
	return fallback
}

func statusViewLogoAssets(logo statusViewLogo) statusViewLogo {
	if terminal.GetLogo(
		logo.product,
		logo.ecosystem,
		logo.version,
		terminal.LogoSmallPlain,
	) == "" {
		return statusViewLogo{}
	}

	smallVariant := terminal.LogoSmallPlain
	largeVariant := terminal.LogoLargePlain
	if style.ColorsEnabled() {
		if terminal.GetLogo(
			logo.product,
			logo.ecosystem,
			logo.version,
			terminal.LogoSmallColored,
		) != "" {
			smallVariant = terminal.LogoSmallColored
		}
		if terminal.GetLogo(
			logo.product,
			logo.ecosystem,
			logo.version,
			terminal.LogoLargeColored,
		) != "" {
			largeVariant = terminal.LogoLargeColored
		}
	}

	logo.small = statusViewLogoLines(terminal.GetLogo(
		logo.product,
		logo.ecosystem,
		logo.version,
		smallVariant,
	))
	logo.large = statusViewLogoLines(terminal.GetLogo(
		logo.product,
		logo.ecosystem,
		logo.version,
		largeVariant,
	))
	if len(logo.large) == 0 {
		logo.large = logo.small
	}
	return logo
}

func statusViewLogoLines(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r", "")
	lines := strings.Split(raw, "\n")
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[0])) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 &&
		strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}

	maxWidth := 0
	for _, line := range lines {
		maxWidth = max(maxWidth, lipgloss.Width(line))
	}
	for i, line := range lines {
		if width := lipgloss.Width(line); width < maxWidth {
			lines[i] = line + strings.Repeat(" ", maxWidth-width)
		}
	}
	return lines
}

func statusViewVariantLines(logo statusViewLogo, large bool) []string {
	if large {
		if len(logo.large) > 0 {
			return logo.large
		}
		return logo.small
	}
	if len(logo.small) > 0 {
		return logo.small
	}
	return logo.large
}

func statusViewCompose(
	info string,
	logo statusViewLogo,
	layout terminal.StatusLayoutParams,
	logoMode terminal.StatusLogoMode,
) string {
	infoLines := statusViewLines(info)

	switch layout.Mode {
	case terminal.LayoutLargeLogoSideBySide:
		return statusViewJoin(statusViewSideBySide(
			infoLines,
			statusViewVariantLines(logo, true),
			layout,
		))
	case terminal.LayoutSmallLogoSideBySide:
		return statusViewJoin(statusViewSideBySide(
			infoLines,
			statusViewVariantLines(logo, false),
			layout,
		))
	case terminal.LayoutVertical:
		logoLines := statusViewVariantLines(
			logo,
			logoMode != terminal.StatusLogoSmall,
		)
		lines := make([]string, 0, len(logoLines)+len(infoLines)+1)
		for _, line := range logoLines {
			lines = append(lines, statusViewClip(line, layout.InfoWidth))
		}
		lines = append(lines, "")
		lines = append(lines, infoLines...)
		return statusViewJoin(lines)
	case terminal.LayoutClipped:
		return statusViewJoin(statusViewClipLines(infoLines, layout.InfoWidth))
	default:
		return statusViewJoin(infoLines)
	}
}

func statusViewSideBySide(
	infoLines []string,
	logoLines []string,
	layout terminal.StatusLayoutParams,
) []string {
	logoWidth := layout.LogoWidth
	if logoWidth <= 0 {
		logoWidth = statusViewBlockWidth(logoLines)
	}
	gapWidth := max(layout.GapWidth, 0)
	gap := strings.Repeat(" ", gapWidth)
	rows := max(len(infoLines), len(logoLines))
	lines := make([]string, 0, rows)

	for i := range rows {
		left := ""
		if i < len(logoLines) {
			left = statusViewClip(logoLines[i], logoWidth)
		}
		right := ""
		if i < len(infoLines) {
			right = infoLines[i]
		}

		var line string
		switch {
		case right == "":
			line = left
		case left == "":
			line = strings.Repeat(" ", logoWidth+gapWidth) + right
		default:
			padding := max(logoWidth-ansi.StringWidth(left), 0)
			line = left +
				strings.Repeat(" ", padding) +
				gap +
				right
		}
		lines = append(lines, statusViewTrim(line))
	}
	return lines
}

func statusViewLines(value string) []string {
	value = strings.TrimRight(value, "\n")
	if value == "" {
		return nil
	}
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		lines[i] = statusViewTrim(line)
	}
	return lines
}

func statusViewJoin(lines []string) string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	for i, line := range lines {
		lines[i] = statusViewTrim(line)
	}
	return strings.Join(lines, "\n")
}

func statusViewTrim(line string) string {
	return strings.TrimRight(line, " \t\r")
}

func statusViewClip(line string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(line) <= width {
		return line
	}
	return ansi.Truncate(line, width, "")
}

func statusViewClipLines(lines []string, width int) []string {
	clipped := make([]string, len(lines))
	for i, line := range lines {
		clipped[i] = statusViewClip(line, width)
	}
	return clipped
}

func statusViewFacts(
	document statusDocument,
	packages []statusViewPackageGroup,
	long bool,
	width int,
) [][]terminal.Fact {
	runtime := []terminal.Fact{}
	serverRow := -1
	if server := document.Server; server != nil {
		if server.Minecraft != "" {
			runtime = append(runtime, terminal.Fact{Key: "Minecraft", Value: server.Minecraft})
		}
		serverRow = len(runtime)
		runtime = append(runtime,
			terminal.Fact{Key: "Server"},
			terminal.Fact{Key: "Activity", Value: statusViewActivity(document.Source, server.Activity)},
		)
	} else {
		runtime = append(runtime, terminal.Fact{Key: "Server", Value: style.Muted("no server found")})
	}
	if document.MCDR != nil {
		runtime = append(runtime, statusViewMCDRFact(document.MCDR))
	}

	contents := []terminal.Fact{}
	if document.Server != nil && len(document.Server.CompatibleEcosystems) > 0 {
		contents = append(contents, terminal.Fact{
			Key:   "Ecosystems",
			Value: statusViewCompatibility(document.Server.CompatibleEcosystems),
		})
	}
	packageStart := len(contents)
	if !long {
		for _, group := range packages {
			contents = append(contents, terminal.Fact{Key: statusViewPackageGroupKey(group)})
		}
	}
	groups := [][]terminal.Fact{runtime, contents}
	valueWidth := statusViewValueWidth(groups, width)
	if serverRow >= 0 {
		runtime[serverRow].Value = statusViewServer(document.Root, document.Server, valueWidth)
	}
	if !long {
		for i, group := range packages {
			contents[packageStart+i].Value = statusViewPackageGrid(group.packages, valueWidth)
		}
	}
	return groups
}

func statusViewValueWidth(groups [][]terminal.Fact, width int) int {
	keyWidth := 0
	for _, group := range groups {
		for _, fact := range group {
			keyWidth = max(keyWidth, ansi.StringWidth(fact.Key))
		}
	}
	keyWidth = min(keyWidth, max(1, width/3))
	return max(1, width-keyWidth-2)
}

func statusViewServer(root string, server *statusServer, width int) string {
	product := terminal.TruncateMiddle(statusViewProduct(server.Product), width)
	value := product
	if server.EntryPoint != "" {
		filename := statusViewLauncher(root, server.EntryPoint)
		remaining := width - ansi.StringWidth(product) - 3
		if remaining >= min(8, ansi.StringWidth(filename)) {
			value += "   " + style.Muted(terminal.TruncateMiddle(filename, remaining))
		} else {
			value += "\n" + style.Muted(terminal.TruncateMiddle(filename, width))
		}
	}
	if components := statusViewComponents(server); components != "" {
		value += "\n" + style.Muted(components)
	}
	return value
}

// Loader installs expose runtime arguments as their entry point. Operators
// start the server through the generated launch script, so show that instead.
func statusViewLauncher(root, entryPoint string) string {
	filename := filepath.Base(terminal.DisplayPath(root, entryPoint))
	if filename != "unix_args.txt" && filename != "win_args.txt" {
		return filename
	}
	for _, candidate := range []string{"run.sh", "run.bat", "start.sh", "start.bat"} {
		if _, err := os.Stat(filepath.Join(root, candidate)); err == nil {
			return candidate
		}
	}
	return filename
}

func statusViewProduct(product statusProduct) string {
	name := strings.TrimSpace(product.Name)
	if parsed, ok := serverpkg.ParseProduct(name); ok {
		name = parsed.Title()
	} else if name != "" {
		name = style.Capitalize(name)
	}

	parts := make([]string, 0, 3)
	if name != "" {
		parts = append(parts, name)
	}
	if version := strings.TrimSpace(product.Version); version != "" {
		parts = append(parts, version)
	}
	if len(parts) == 0 {
		return "unknown"
	}
	return strings.Join(parts, " ")
}

func statusViewActivity(source string, activity string) string {
	activity = strings.ToLower(strings.TrimSpace(activity))
	if strings.EqualFold(strings.TrimSpace(source), statusSourceLock) {
		return style.Muted("not observed")
	}
	switch activity {
	case statusActivityRunning:
		return style.Success("● running")
	case statusActivityStopped:
		return style.Muted("○ stopped")
	case "", statusActivityUnknown:
		return style.Muted("unknown")
	default:
		return "○ " + activity
	}
}

func statusViewProvider(provider string) string {
	provider = strings.TrimSpace(provider)
	if strings.EqualFold(provider, "any") || strings.EqualFold(provider, "unknown") {
		return ""
	}
	return provider
}

func statusViewComponents(server *statusServer) string {
	lines := make([]string, 0, len(server.Components))
	for _, component := range server.Components {
		minecraft := component.Ecosystem == "minecraft" && component.Version == server.Minecraft
		primary := component.Ecosystem == server.Product.Ecosystem && component.Version == server.Product.Version
		if minecraft || primary {
			continue
		}
		lines = append(lines, statusViewProduct(statusProduct{
			Name:    component.Name,
			Version: component.Version,
		}))
	}
	return strings.Join(lines, ", ")
}

func statusViewCompatibility(ecosystems []statusEcosystem) string {
	labels := make([]string, 0, len(ecosystems))
	for _, offer := range ecosystems {
		title := strings.TrimSpace(offer.Ecosystem)
		if title == "" {
			continue
		}
		if parsed := catalog.Ecosystem(strings.ToLower(title)); parsed.Valid() {
			title = parsed.Title()
		}
		if compatibility := strings.TrimSpace(offer.Compatibility); compatibility != "" {
			title += " " + style.Muted("("+compatibility+")")
		}
		labels = append(labels, title)
	}
	return strings.Join(labels, ", ")
}

func statusViewPackageGroups(packages []statusPackage) []statusViewPackageGroup {
	mods := make([]statusPackage, 0, len(packages))
	plugins := make([]statusPackage, 0, len(packages))
	mcdr := make([]statusPackage, 0, len(packages))
	for _, pkg := range packages {
		ecosystem := catalog.Ecosystem(strings.ToLower(pkg.Ecosystem))
		switch {
		case strings.EqualFold(
			pkg.Environment,
			string(serverpkg.EnvironmentMCDR),
		) || ecosystem == catalog.EcosystemMCDR:
			mcdr = append(mcdr, pkg)
		case ecosystem.IsModding():
			mods = append(mods, pkg)
		default:
			plugins = append(plugins, pkg)
		}
	}

	groups := make([]statusViewPackageGroup, 0, 3)
	for _, group := range []statusViewPackageGroup{
		{title: "Mods", packages: mods},
		{title: "Plugins", packages: plugins},
		{title: "MCDR plugins", packages: mcdr},
	} {
		if len(group.packages) > 0 {
			slices.SortFunc(group.packages, func(a, b statusPackage) int {
				return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Path, b.Path))
			})
			groups = append(groups, group)
		}
	}
	return groups
}

func statusViewPackageGroupKey(group statusViewPackageGroup) string {
	return fmt.Sprintf("%s (%d)", group.title, len(group.packages))
}

func statusViewPackageTable(root string, packages []statusPackage, width int) string {
	width = max(1, width-2)
	nameWidth, versionWidth := 1, 1
	for _, pkg := range packages {
		nameWidth = max(nameWidth, ansi.StringWidth(statusViewPackageName(pkg)))
		versionWidth = max(versionWidth, ansi.StringWidth(pkg.Version))
	}
	nameWidth = min(nameWidth, max(1, width*2/5))
	versionWidth = min(versionWidth, max(7, width/6))
	gap := statusViewColumnGap
	if width < 12 {
		gap = 1
	}
	pathWidth := max(1, width-nameWidth-versionWidth-2*gap)
	var table strings.Builder
	for i, pkg := range packages {
		if i > 0 {
			table.WriteByte('\n')
		}
		table.WriteString("  ")
		name := terminal.TruncateMiddle(statusViewPackageName(pkg), nameWidth)
		if width < 5 {
			table.WriteString(terminal.TruncateMiddle(name, width))
			continue
		}
		version := strings.TrimSpace(pkg.Version)
		if version == "" {
			version = "—"
		}
		version = terminal.TruncateMiddle(version, versionWidth)
		path := terminal.DisplayPath(root, pkg.Path)
		if path == "" {
			path = "—"
		}
		table.WriteString(name)
		table.WriteString(strings.Repeat(" ", nameWidth-ansi.StringWidth(name)+gap))
		table.WriteString(version)
		table.WriteString(strings.Repeat(" ", versionWidth-ansi.StringWidth(version)+gap))
		table.WriteString(style.Muted(terminal.TruncateMiddle(path, pathWidth)))
	}
	return table.String()
}

func statusViewPackageGrid(packages []statusPackage, width int) string {
	labels := statusViewPackageLabels(packages)
	if len(labels) == 0 {
		return ""
	}

	cellBudget := max(18, (width-2*statusViewColumnGap)/3)
	longest := 0
	for i, label := range labels {
		labels[i] = terminal.TruncateMiddle(label, max(1, min(width, cellBudget)))
		longest = max(longest, ansi.StringWidth(labels[i]))
	}
	columnWidth := longest + statusViewColumnGap
	columns := (width + statusViewColumnGap) / max(columnWidth, 1)
	columns = max(1, min(columns, len(labels)))

	visible := min(len(labels), columns*statusViewPackageGridLines)
	lines := make([]string, 0, statusViewPackageGridLines+1)
	for start := 0; start < visible; start += columns {
		end := min(start+columns, visible)
		var row strings.Builder
		for i := start; i < end; i++ {
			row.WriteString(labels[i])
			if i < end-1 {
				padding := columnWidth - ansi.StringWidth(labels[i])
				row.WriteString(strings.Repeat(" ", max(padding, 1)))
			}
		}
		lines = append(lines, row.String())
	}
	if hidden := len(labels) - visible; hidden > 0 {
		lines = append(lines, terminal.FoldHint(hidden))
	}
	return strings.Join(lines, "\n")
}

func statusViewPackageLabels(packages []statusPackage) []string {
	counts := make(map[string]int, len(packages))
	for _, pkg := range packages {
		counts[statusViewPackageName(pkg)]++
	}

	labels := make([]string, 0, len(packages))
	for _, pkg := range packages {
		name := statusViewPackageName(pkg)
		label := name
		if counts[name] > 1 {
			if provider := statusViewProvider(pkg.Provider); provider != "" {
				label = provider + ":" + name
			} else if ecosystem := strings.TrimSpace(pkg.Ecosystem); ecosystem != "" {
				label = ecosystem + "/" + name
			}
		}
		labels = append(labels, label)
	}
	return labels
}

func statusViewPackageName(pkg statusPackage) string {
	if name := strings.TrimSpace(pkg.Name); name != "" {
		return name
	}
	if provider := statusViewProvider(pkg.Provider); provider != "" {
		return provider
	}
	if ecosystem := strings.TrimSpace(pkg.Ecosystem); ecosystem != "" {
		return ecosystem
	}
	return "unknown"
}

func statusViewMCDRFact(mcdr *statusMCDR) terminal.Fact {
	value := strings.TrimSpace(mcdr.Version)
	if value == "" {
		value = "installed"
	}
	return terminal.Fact{Key: "MCDR", Value: value}
}
