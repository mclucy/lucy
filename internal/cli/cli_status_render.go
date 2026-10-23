package cli

import (
	"fmt"
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

	facts := statusViewFacts(document, long, layout.InfoWidth)
	info := terminal.RenderFacts(facts, layout.InfoWidth)
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
	long bool,
	width int,
) []terminal.Fact {
	facts := []terminal.Fact{
		{Key: "Source", Value: statusViewSource(document.Source)},
		{Key: "Root", Value: statusViewRoot(document.Root)},
	}

	if server := document.Server; server != nil {
		facts = append(facts, terminal.Fact{
			Key:   "Server",
			Value: statusViewProduct(server.Product),
		})
		if server.Minecraft != "" {
			facts = append(facts, terminal.Fact{
				Key:   "Minecraft",
				Value: server.Minecraft,
			})
		}
		if server.EntryPoint != "" {
			facts = append(facts, terminal.Fact{
				Key:   "Entry point",
				Value: terminal.DisplayPath(document.Root, server.EntryPoint),
			})
		}
		facts = append(facts, terminal.Fact{
			Key:   "Activity",
			Value: statusViewActivity(document.Source, server.Activity),
		})
		if components := statusViewComponents(server, long); components != "" {
			facts = append(facts, terminal.Fact{
				Key:   "Components",
				Value: components,
			})
		}
		if len(server.CompatibleEcosystems) > 0 {
			facts = append(facts, terminal.Fact{
				Key:   "Compatible",
				Value: statusViewCompatibility(server.CompatibleEcosystems),
			})
		}
	} else {
		facts = append(facts, terminal.Fact{
			Key:   "Server",
			Value: style.Muted("(no server found)"),
		})
	}

	groups := statusViewPackageGroups(document.Packages)
	tail := make([]terminal.Fact, 0, 3)
	if document.MCDR != nil {
		tail = append(tail, statusViewMCDRFact(document.Root, document.MCDR))
	}
	if document.Server != nil && len(document.Server.PackageDirectories) > 0 {
		tail = append(tail, terminal.Fact{
			Key: "Package dirs",
			Value: statusViewPackageDirectories(
				document.Root,
				document.Server.PackageDirectories,
			),
		})
	}

	keys := make([]string, 0, len(facts)+len(groups)+len(tail)+1)
	for _, fact := range facts {
		keys = append(keys, fact.Key)
	}
	keys = append(keys, "Packages")
	for _, group := range groups {
		keys = append(keys, statusViewPackageGroupKey(group))
	}
	for _, fact := range tail {
		keys = append(keys, fact.Key)
	}
	valueWidth := statusViewValueWidth(keys, width)

	if len(document.Packages) == 0 {
		facts = append(facts, terminal.Fact{Key: "Packages", Value: "none"})
	} else {
		facts = append(facts, terminal.Fact{
			Key:   "Packages",
			Value: fmt.Sprintf("%d total", len(document.Packages)),
		})
		for _, group := range groups {
			facts = append(facts, terminal.Fact{
				Key: statusViewPackageGroupKey(group),
				Value: statusViewPackageGroupValue(
					document.Root,
					group,
					long,
					valueWidth,
				),
			})
		}
	}

	facts = append(facts, tail...)
	for i := range facts {
		switch facts[i].Key {
		case "Root", "Entry point", "Package dirs":
			paths := strings.Split(facts[i].Value, "\n")
			for j, path := range paths {
				if long {
					paths[j] = strings.Join(statusViewWrap(path, valueWidth, "/"), "\n")
				} else {
					paths[j] = terminal.TruncateMiddle(path, valueWidth)
				}
			}
			facts[i].Value = strings.Join(paths, "\n")
		}
	}
	return facts
}

// statusViewValueWidth mirrors RenderFacts' width arithmetic so package cells
// and long references are wrapped before RenderFacts has to hard-wrap them.
func statusViewValueWidth(keys []string, width int) int {
	keyWidth := 0
	for _, key := range keys {
		keyWidth = max(keyWidth, ansi.StringWidth(key))
	}
	keyWidth = min(keyWidth, max(1, width/3))
	return max(1, width-keyWidth-2)
}

func statusViewSource(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	switch source {
	case statusSourceLive, statusSourceLock:
		return source
	case "":
		return "unknown"
	default:
		return source
	}
}

func statusViewRoot(root string) string {
	if root == "" {
		return "."
	}
	return terminal.DisplayPath("", root)
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
		return style.Muted("unobserved (lock)")
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

func statusViewComponents(server *statusServer, long bool) string {
	lines := make([]string, 0, len(server.Components))
	for _, component := range server.Components {
		minecraft := component.Ecosystem == "minecraft" && component.Version == server.Minecraft
		primary := component.Ecosystem == server.Product.Ecosystem && component.Version == server.Product.Version
		if !long && (minecraft || primary) {
			continue
		}
		name := strings.TrimSpace(component.Name)
		if name == "" {
			name = "unknown"
		}
		reference := name
		if provider := statusViewProvider(component.Provider); provider != "" {
			reference = provider + ":" + name
		}
		if version := strings.TrimSpace(component.Version); version != "" {
			reference += "@" + version
		}
		if ecosystem := strings.TrimSpace(component.Ecosystem); long && ecosystem != "" {
			reference += " (" + ecosystem + ")"
		}
		lines = append(lines, reference)
	}
	return strings.Join(lines, "\n")
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
			title += " (" + compatibility + ")"
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
		{title: "MCDR", packages: mcdr},
	} {
		if len(group.packages) > 0 {
			groups = append(groups, group)
		}
	}
	return groups
}

func statusViewPackageGroupKey(group statusViewPackageGroup) string {
	return fmt.Sprintf("%s (%d)", group.title, len(group.packages))
}

func statusViewPackageGroupValue(
	root string,
	group statusViewPackageGroup,
	long bool,
	width int,
) string {
	if long {
		return statusViewLongPackages(root, group.packages, width)
	}
	return statusViewPackageGrid(group.packages, width)
}

func statusViewLongPackages(
	root string,
	packages []statusPackage,
	width int,
) string {
	lines := make([]string, 0, len(packages)*2)
	for _, pkg := range packages {
		reference := statusViewPackageReference(pkg)
		path := terminal.DisplayPath(root, pkg.Path)
		if path == "" {
			lines = append(lines, statusViewWrap(reference, width, ":@-")...)
			continue
		}
		if ansi.StringWidth(reference)+statusViewColumnGap+
			ansi.StringWidth(path) <= width {
			lines = append(lines, reference+"  "+path)
			continue
		}

		lines = append(lines, statusViewWrap(reference, width, ":@-")...)
		indent := strings.Repeat(" ", statusViewColumnGap)
		pathWidth := width - statusViewColumnGap
		if pathWidth < 1 {
			indent = ""
			pathWidth = width
		}
		for _, pathLine := range statusViewWrap(path, pathWidth, "/-_.") {
			lines = append(lines, indent+pathLine)
		}
	}
	return strings.Join(lines, "\n")
}

// statusViewWrap wraps on separators first while preserving every character.
func statusViewWrap(value string, width int, breakpoints string) []string {
	if width < 1 {
		return []string{value}
	}
	return strings.Split(ansi.Wrap(value, width, breakpoints), "\n")
}

func statusViewPackageGrid(packages []statusPackage, width int) string {
	labels := statusViewPackageLabels(packages)
	if len(labels) == 0 {
		return ""
	}

	longest := 0
	for _, label := range labels {
		longest = max(longest, ansi.StringWidth(label))
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

func statusViewPackageReference(pkg statusPackage) string {
	name := strings.TrimSpace(pkg.Name)
	if name == "" {
		name = statusViewPackageName(pkg)
	}
	reference := name
	if provider := statusViewProvider(pkg.Provider); provider != "" {
		reference = provider + ":" + name
	} else if ecosystem := strings.TrimSpace(pkg.Ecosystem); ecosystem != "" {
		reference = ecosystem + ":" + name
	}
	if version := strings.TrimSpace(pkg.Version); version != "" {
		reference += "@" + version
	}
	return reference
}

func statusViewMCDRFact(root string, mcdr *statusMCDR) terminal.Fact {
	parts := make([]string, 0, 3)
	if version := strings.TrimSpace(mcdr.Version); version != "" {
		parts = append(parts, version)
	}
	if python := strings.TrimSpace(mcdr.Python); python != "" {
		parts = append(parts, "Python "+python)
	}
	if target := strings.TrimSpace(mcdr.Target); target != "" {
		parts = append(parts, "target "+target)
	}

	value := "installed"
	if len(parts) > 0 {
		value = strings.Join(parts, " · ")
	}
	if directory := terminal.DisplayPath(root, mcdr.Directory); directory != "" {
		value += "\ndirectory " + directory
	}
	return terminal.Fact{Key: "MCDR", Value: value}
}

func statusViewPackageDirectories(root string, directories []string) string {
	paths := make([]string, 0, len(directories))
	for _, directory := range directories {
		if path := terminal.DisplayPath(root, directory); path != "" {
			paths = append(paths, path)
		}
	}
	return strings.Join(paths, ", ")
}
