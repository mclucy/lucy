package info

import (
	"net/url"
	"os"
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mclucy/lucy/terminal"
	"github.com/mclucy/lucy/terminal/style"
)

// Markdown renderers emit OSC 8 links for arbitrary targets. Prose keeps its
// styled text, but hyperlinks stay exclusive to the vetted Links fact.
var infoRenderHyperlink = regexp.MustCompile(
	"\x1b\\]8;[^\x07\x1b]*(?:\x07|\x1b\\\\)",
)

const (
	infoRenderDefaultWidth        = 80
	infoRenderFallbackHeight      = 24
	infoRenderDefaultLinks        = 4
	infoRenderDefaultDependencies = 5
	infoRenderDescriptionMinLines = 3
	infoRenderDescriptionMaxLines = 24
	infoRenderEntryMinHeight      = 8
	infoRenderIndent              = "  "

	infoRenderUnknownDependencies = "unknown (not reported by this source)"
	infoRenderUnknownRequirements = "unknown (additional requirements may apply)"
)

// renderInfoDocument renders the human info view from the same document that
// produces JSON. Warnings stay in the document and reach the user through the
// command's warning stream, matching the status view.
func renderInfoDocument(
	document infoDocument,
	long bool,
	width int,
	tty bool,
) string {
	if width <= 0 {
		width = infoRenderDefaultWidth
	}
	if len(document.Entries) == 0 {
		return ""
	}

	height := style.TermHeight()
	if !tty || height <= 0 {
		height = infoRenderFallbackHeight
	}
	share := max(infoRenderEntryMinHeight, height/len(document.Entries))

	root, _ := os.Getwd()
	file := ""
	if document.File != "" {
		file = terminal.DisplayPath(
			root,
			strings.TrimSpace(infoRenderText(document.File)),
		)
	}
	blocks := make([]string, 0, len(document.Entries))
	for index, entry := range document.Entries {
		entryFile := ""
		if index == 0 {
			entryFile = file
		}
		blocks = append(
			blocks,
			renderInfoEntry(entry, long, width, tty, share, root, entryFile),
		)
	}
	return infoRenderTrim(strings.Join(blocks, "\n\n"))
}

func renderInfoEntry(
	entry infoEntry,
	long bool,
	width int,
	tty bool,
	height int,
	root string,
	file string,
) string {
	title := infoRenderInline(entry.Title)
	if title == "" {
		title = infoRenderInline(entry.Ref)
	}
	if title == "" && entry.Identity != nil {
		title = infoRenderInline(entry.Identity.Name)
	}
	if title == "" {
		title = "unnamed"
	}
	titleLines := strings.Split(lipgloss.Wrap(title, width, ""), "\n")
	for i := range titleLines {
		titleLines[i] = style.Accent(titleLines[i])
	}

	summary := infoRenderInline(entry.Summary)
	var summaryLines []string
	if summary != "" && summary != title {
		summaryLines = strings.Split(lipgloss.Wrap(summary, width, ""), "\n")
	}

	facts := infoRenderEntryFacts(entry, long, width, tty, root, file)
	factWidth := width
	if !tty {
		keyWidth, valueWidth := 0, 0
		for _, fact := range facts {
			keyWidth = max(keyWidth, ansi.StringWidth(fact.Key))
			for _, line := range strings.Split(fact.Value, "\n") {
				valueWidth = max(valueWidth, ansi.StringWidth(line))
			}
		}
		factWidth = max(factWidth, keyWidth+2+valueWidth)
	}
	factsText := terminal.RenderFacts(facts, factWidth)
	var factLines []string
	if factsText != "" {
		factLines = strings.Split(factsText, "\n")
	}

	lines := make([]string, 0, len(titleLines)+len(summaryLines)+len(factLines)+8)
	lines = append(lines, titleLines...)
	lines = append(lines, summaryLines...)
	if len(factLines) > 0 {
		lines = append(lines, "")
		lines = append(lines, factLines...)
	}

	description := infoRenderDescription(entry, width, long, height-len(lines)-2)
	if description != "" {
		lines = append(lines, "", style.Accent("About"))
		lines = append(lines, infoRenderIndentLines(description)...)
	}
	return strings.Join(lines, "\n")
}

func infoRenderEntryFacts(
	entry infoEntry,
	long bool,
	width int,
	tty bool,
	root string,
	file string,
) []terminal.Fact {
	ref := infoRenderInline(entry.Ref)
	license := ""
	if long {
		license = infoRenderInline(entry.License)
	}
	authors := ""
	if long {
		authors = infoRenderAuthors(entry.Authors, tty)
	}
	identity := ""
	if long {
		identity = infoRenderIdentity(entry.Identity)
	}
	upstream := ""
	if long {
		upstream = infoRenderUpstream(entry.Upstream)
	}

	needs := infoRenderActionableDependencies(entry.Needs, long)
	optional := infoRenderActionableDependencies(entry.Optional, long)
	bundled := infoRenderDistributionDependencies(entry.Dependencies, "bundled", long)
	server := infoRenderDistributionDependencies(entry.Dependencies, "server", long)
	leftover := infoRenderLeftoverDependencies(entry, needs, optional, long)
	links := infoRenderLinkCandidates(entry.Links, long)

	keys := make([]string, 0, 13)
	if file != "" {
		keys = append(keys, "File")
	}
	if ref != "" {
		keys = append(keys, "Ref")
	}
	if entry.Installed != nil {
		keys = append(keys, "Installed")
	}
	if license != "" {
		keys = append(keys, "License")
	}
	if authors != "" {
		keys = append(keys, "Authors")
	}
	if identity != "" {
		keys = append(keys, "Identity")
	}
	if upstream != "" {
		keys = append(keys, "Upstream")
	}
	keys = append(keys, "Needs")
	if len(optional) > 0 {
		keys = append(keys, "Optional")
	}
	if len(bundled) > 0 {
		keys = append(keys, "Bundled")
	}
	if len(server) > 0 {
		keys = append(keys, "Server")
	}
	if len(leftover) > 0 {
		keys = append(keys, "Dependencies")
	}
	if len(links) > 0 {
		keys = append(keys, "Links")
	}
	valueWidth := infoRenderValueWidth(keys, width)

	facts := make([]terminal.Fact, 0, len(keys))
	if file != "" {
		facts = append(facts, terminal.Fact{
			Key:   "File",
			Value: ansi.Wrap(file, valueWidth, "/-_."),
		})
	}
	if ref != "" {
		facts = append(facts, terminal.Fact{
			Key:   "Ref",
			Value: ansi.Wrap(ref, valueWidth, ":@-/"),
		})
	}
	if entry.Installed != nil {
		facts = append(facts, terminal.Fact{
			Key: "Installed",
			Value: infoRenderInstalled(
				entry.Installed,
				ref,
				valueWidth,
				root,
			),
		})
	}
	if license != "" {
		facts = append(facts, terminal.Fact{Key: "License", Value: license})
	}
	if authors != "" {
		facts = append(facts, terminal.Fact{Key: "Authors", Value: authors})
	}
	if identity != "" {
		facts = append(facts, terminal.Fact{Key: "Identity", Value: identity})
	}
	if upstream != "" {
		facts = append(facts, terminal.Fact{Key: "Upstream", Value: upstream})
	}
	needsNote := ""
	if !entry.DependenciesKnown {
		needsNote = infoRenderUnknownDependencies
		if len(needs) > 0 {
			needsNote = infoRenderUnknownRequirements
		}
	}
	needsValue := "none"
	if len(needs) > 0 || needsNote != "" {
		needsValue = infoRenderDependencyValue(needs, long, needsNote)
	}
	facts = append(facts, terminal.Fact{Key: "Needs", Value: needsValue})
	if len(optional) > 0 {
		facts = append(facts, terminal.Fact{
			Key:   "Optional",
			Value: infoRenderDependencyValue(optional, long, ""),
		})
	}
	if len(bundled) > 0 {
		facts = append(facts, terminal.Fact{
			Key:   "Bundled",
			Value: infoRenderDependencyValue(bundled, true, ""),
		})
	}
	if len(server) > 0 {
		facts = append(facts, terminal.Fact{
			Key:   "Server",
			Value: infoRenderDependencyValue(server, true, ""),
		})
	}
	if len(leftover) > 0 {
		facts = append(facts, terminal.Fact{
			Key:   "Dependencies",
			Value: infoRenderDependencyValue(leftover, true, ""),
		})
	}
	if len(links) > 0 {
		facts = append(facts, terminal.Fact{
			Key:   "Links",
			Value: infoRenderLinks(links, long, valueWidth, tty),
		})
	}
	return facts
}

// infoRenderValueWidth mirrors RenderFacts' width arithmetic so values that
// must not break mid-token, such as paths and references, wrap beforehand.
func infoRenderValueWidth(keys []string, width int) int {
	keyWidth := 0
	for _, key := range keys {
		keyWidth = max(keyWidth, ansi.StringWidth(key))
	}
	keyWidth = min(keyWidth, max(1, width/3))
	return max(1, width-keyWidth-2)
}

func infoRenderInstalled(
	installed *infoInstalled,
	entryRef string,
	valueWidth int,
	root string,
) string {
	version := infoRenderInline(installed.Version)
	ref := infoRenderInline(installed.Ref)
	if ref == entryRef {
		ref = ""
	}
	head := version
	if ref != "" {
		if head == "" {
			head = ref
		} else {
			head += " (" + ref + ")"
		}
	}

	path := terminal.DisplayPath(
		root,
		strings.TrimSpace(infoRenderText(installed.Path)),
	)
	if path != "" {
		path = ansi.Wrap(path, valueWidth, "/-_.")
	}
	switch {
	case head != "" && path != "":
		return head + "\n" + path
	case head != "":
		return head
	case path != "":
		return path
	default:
		return "yes"
	}
}

func infoRenderIdentity(identity *infoIdentity) string {
	if identity == nil {
		return ""
	}
	name := infoRenderInline(identity.Name)
	version := infoRenderInline(identity.Version)
	head := strings.TrimSpace(name + " " + version)
	details := make([]string, 0, 2)
	if provider := infoRenderInline(identity.Provider); provider != "" {
		details = append(details, provider)
	}
	if ecosystem := infoRenderInline(identity.Ecosystem); ecosystem != "" {
		details = append(details, ecosystem)
	}
	if len(details) == 0 {
		return head
	}
	if head == "" {
		return strings.Join(details, ", ")
	}
	return head + " (" + strings.Join(details, ", ") + ")"
}

func infoRenderUpstream(upstream *infoUpstream) string {
	if upstream == nil {
		return ""
	}
	ref := infoRenderInline(upstream.Ref)
	version := infoRenderInline(upstream.Version)
	switch {
	case ref != "" && version != "":
		return ref + " (" + version + ")"
	default:
		return strings.TrimSpace(ref + " " + version)
	}
}

func infoRenderAuthors(authors []infoAuthor, tty bool) string {
	lines := make([]string, 0, len(authors))
	for _, author := range authors {
		name := infoRenderInline(author.Name)
		if name == "" {
			continue
		}
		line := name
		if role := infoRenderInline(author.Role); role != "" {
			line += " (" + role + ")"
		}
		if raw := infoRenderInline(author.URL); raw != "" {
			link := raw
			if safe, ok := infoRenderSafeURL(raw); ok && tty {
				link = ansi.SetHyperlink(safe) +
					style.Link(raw) +
					ansi.ResetHyperlink()
			}
			line += "  " + link
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// infoRenderActionableDependencies drops embedded and server-provided entries
// from the default actionable lists. Long output keeps them with full detail.
func infoRenderActionableDependencies(
	dependencies []infoDependency,
	long bool,
) []infoDependency {
	visible := make([]infoDependency, 0, len(dependencies))
	for _, dependency := range dependencies {
		if infoRenderInline(dependency.Ref) == "" {
			continue
		}
		if !long && infoRenderEmbeddedDependency(dependency) {
			continue
		}
		visible = append(visible, dependency)
	}
	return visible
}

func infoRenderEmbeddedDependency(dependency infoDependency) bool {
	switch strings.ToLower(infoRenderInline(dependency.Distribution)) {
	case "bundled", "server":
		return true
	default:
		return false
	}
}

func infoRenderDistributionDependencies(
	dependencies []infoDependency,
	distribution string,
	long bool,
) []infoDependency {
	if !long {
		return nil
	}
	matches := make([]infoDependency, 0, len(dependencies))
	for _, dependency := range dependencies {
		if infoRenderInline(dependency.Ref) == "" {
			continue
		}
		if !strings.EqualFold(
			infoRenderInline(dependency.Distribution),
			distribution,
		) {
			continue
		}
		matches = append(matches, dependency)
	}
	return matches
}

// infoRenderLeftoverDependencies reports long-detail dependencies the
// actionable lists and distribution groups did not already cover.
func infoRenderLeftoverDependencies(
	entry infoEntry,
	needs []infoDependency,
	optional []infoDependency,
	long bool,
) []infoDependency {
	if !long {
		return nil
	}
	shown := make(map[string]struct{}, len(needs)+len(optional))
	for _, dependency := range needs {
		shown[infoRenderDependencyKey(dependency)] = struct{}{}
	}
	for _, dependency := range optional {
		shown[infoRenderDependencyKey(dependency)] = struct{}{}
	}
	leftover := make([]infoDependency, 0, len(entry.Dependencies))
	for _, dependency := range entry.Dependencies {
		if infoRenderInline(dependency.Ref) == "" {
			continue
		}
		if infoRenderEmbeddedDependency(dependency) {
			continue
		}
		if _, ok := shown[infoRenderDependencyKey(dependency)]; ok {
			continue
		}
		leftover = append(leftover, dependency)
	}
	return leftover
}

func infoRenderDependencyKey(dependency infoDependency) string {
	return infoRenderInline(dependency.Ref) + "\x00" +
		infoRenderInline(dependency.Constraint)
}

func infoRenderDependencyValue(
	dependencies []infoDependency,
	long bool,
	note string,
) string {
	lines := make([]string, 0, len(dependencies)+2)
	for _, dependency := range dependencies {
		lines = append(lines, infoRenderDependency(dependency, long))
	}
	hidden := 0
	if !long && len(lines) > infoRenderDefaultDependencies {
		hidden = len(lines) - infoRenderDefaultDependencies
		lines = lines[:infoRenderDefaultDependencies]
	}
	if note != "" {
		lines = append(lines, style.Muted(note))
	}
	if hidden > 0 {
		lines = append(lines, terminal.FoldHint(hidden))
	}
	return strings.Join(lines, "\n")
}

func infoRenderDependency(dependency infoDependency, long bool) string {
	ref := infoRenderInline(dependency.Ref)
	if ref == "" {
		ref = "unknown"
	}
	if !long {
		return ref
	}
	notes := make([]string, 0, 3)
	if constraint := infoRenderInline(dependency.Constraint); constraint != "" {
		notes = append(notes, constraint)
	}
	if distribution := infoRenderInline(dependency.Distribution); distribution != "" {
		notes = append(notes, distribution)
	}
	if relationship := infoRenderInline(dependency.Relationship); relationship != "" {
		notes = append(notes, relationship)
	} else if !dependency.Required {
		notes = append(notes, "optional")
	}
	if len(notes) == 0 {
		return ref
	}
	return ref + " (" + strings.Join(notes, ", ") + ")"
}

func infoRenderLinkCandidates(links []infoLink, long bool) []infoLink {
	candidates := make([]infoLink, 0, len(links))
	seen := make(map[string]struct{}, len(links))
	for _, link := range links {
		raw := infoRenderInline(link.URL)
		if raw == "" {
			continue
		}
		if !long && strings.EqualFold(infoRenderInline(link.Kind), "funding") {
			continue
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		candidates = append(candidates, link)
	}
	return candidates
}

func infoRenderLinks(
	links []infoLink,
	long bool,
	valueWidth int,
	tty bool,
) string {
	visible := links
	hidden := 0
	if !long && len(visible) > infoRenderDefaultLinks {
		hidden = len(visible) - infoRenderDefaultLinks
		visible = visible[:infoRenderDefaultLinks]
	}
	lines := make([]string, 0, len(visible)+1)
	for _, link := range visible {
		lines = append(lines, infoRenderLinkLine(link, valueWidth, tty))
	}
	if hidden > 0 {
		lines = append(lines, terminal.FoldHint(hidden))
	}
	return strings.Join(lines, "\n")
}

func infoRenderLinkLine(link infoLink, width int, tty bool) string {
	label := infoRenderInline(link.Label)
	raw := infoRenderInline(link.URL)
	if label == raw {
		label = ""
	}
	if raw == "" {
		return label
	}

	target, safe := infoRenderSafeURL(raw)
	display := raw
	if tty {
		budget := max(1, width)
		if label != "" {
			label = terminal.TruncateMiddle(
				label,
				max(1, min(ansi.StringWidth(label), max(1, width/3))),
			)
			budget = max(1, width-ansi.StringWidth(label)-2)
		}
		display = terminal.TruncateMiddle(display, budget)
		if safe {
			display = ansi.SetHyperlink(target) +
				style.Link(display) +
				ansi.ResetHyperlink()
		}
	}
	if label == "" {
		return display
	}
	return label + "  " + display
}

// infoRenderSafeURL accepts only printable HTTP(S) URLs for OSC 8 output.
func infoRenderSafeURL(raw string) (string, bool) {
	if raw == "" || strings.ContainsAny(raw, " \t\r\n") {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return "", false
	}
	if parsed.Host == "" {
		return "", false
	}
	return raw, true
}

func infoRenderDescription(
	entry infoEntry,
	width int,
	long bool,
	budget int,
) string {
	description := strings.TrimSpace(infoRenderText(entry.Description))
	if description == "" {
		return ""
	}

	contentWidth := max(1, width-len(infoRenderIndent))
	var rendered string
	switch strings.ToLower(infoRenderInline(entry.DescriptionFormat)) {
	case "markdown":
		rendered = style.MarkdownToAnsi(description, contentWidth)
		rendered = infoRenderHyperlink.ReplaceAllString(rendered, "")
	default:
		rendered = lipgloss.Wrap(description, contentWidth, "")
	}
	if rendered == "" {
		return ""
	}
	if !long {
		budget = min(
			max(budget, infoRenderDescriptionMinLines),
			infoRenderDescriptionMaxLines,
		)
		rendered = terminal.CenterElide(rendered, budget)
	}
	return infoRenderTrim(rendered)
}

func infoRenderIndentLines(text string) []string {
	lines := strings.Split(text, "\n")
	for i := range lines {
		if strings.TrimSpace(ansi.Strip(lines[i])) == "" {
			lines[i] = ""
			continue
		}
		lines[i] = infoRenderIndent + lines[i]
	}
	return lines
}

// infoRenderText removes upstream terminal controls while keeping the
// intentional style sequences this renderer adds afterwards.
func infoRenderText(value string) string {
	value = ansi.Strip(value)
	var clean strings.Builder
	clean.Grow(len(value))
	for _, character := range value {
		switch {
		case character == '\n':
			clean.WriteRune(character)
		case character == '\t':
			clean.WriteString("    ")
		case character < 0x20 || (character >= 0x7f && character <= 0x9f):
			continue
		default:
			clean.WriteRune(character)
		}
	}
	return clean.String()
}

func infoRenderInline(value string) string {
	return strings.Join(strings.Fields(infoRenderText(value)), " ")
}

func infoRenderTrim(value string) string {
	lines := strings.Split(value, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t\r")
	}
	for len(lines) > 0 &&
		strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	for len(lines) > 0 &&
		strings.TrimSpace(ansi.Strip(lines[0])) == "" {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}
