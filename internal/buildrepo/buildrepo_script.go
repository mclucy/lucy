package buildrepo

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/types"
)

var pluginEcosystems = []struct {
	id    string
	ecos  []types.Ecosystem
	multi bool
}{
	{id: "dev.architectury.loom", multi: true},
	{id: "architectury.loom", multi: true},
	{id: "architectury-plugin", multi: true},
	{id: "net.fabricmc.fabric-loom", ecos: []types.Ecosystem{types.EcoFabric}},
	{id: "net.fabricmc.fabric-loom-remap", ecos: []types.Ecosystem{types.EcoFabric}},
	{id: "fabric-loom", ecos: []types.Ecosystem{types.EcoFabric}},
	{id: "net.minecraftforge.gradle", ecos: []types.Ecosystem{types.EcoForge}},
	{id: "net.neoforged.moddev.legacyforge", ecos: []types.Ecosystem{types.EcoForge}},
	{id: "net.neoforged.moddev", ecos: []types.Ecosystem{types.EcoNeoforge}},
	{id: "net.neoforged.gradle.userdev", ecos: []types.Ecosystem{types.EcoNeoforge}},
}

var (
	applyFalsePattern  = regexp.MustCompile(`\bapply\s*\(?\s*false\b`)
	idKeywordPattern   = regexp.MustCompile(`\bid\b`)
	applyPluginPattern = regexp.MustCompile(`\bapply\s*\(?\s*plugin\s*[:=]\s*`)
	aliasPattern       = regexp.MustCompile(`\balias\s*(?:\(\s*)?([A-Za-z0-9_.]+)\.plugins\.([A-Za-z0-9_.]+)`)
)

// pluginID reads the id declaration of a single plugins block entry. It
// returns the id and the remainder of the entry, which carries the version and
// any "apply false" modifier.
func pluginID(entry string) (string, string, bool) {
	from := 0
	for {
		loc := idKeywordPattern.FindStringIndex(entry[from:])
		if loc == nil {
			return "", "", false
		}
		index := from + loc[1]
		for index < len(entry) && isSpace(entry[index]) {
			index++
		}
		if index < len(entry) && entry[index] == '(' {
			index++
			for index < len(entry) && isSpace(entry[index]) {
				index++
			}
		}
		if id, width, ok := readStringLiteral(entry[index:]); ok {
			return id, entry[index+width:], true
		}
		from = index
	}
}

func isSpace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\r' || char == '\n'
}

type appliedPlugin struct {
	id     string
	line   int
	source string
}

func (p *prober) scriptEcosystems(file string, plugins []appliedPlugin) []types.Ecosystem {
	var out []types.Ecosystem
	for _, plugin := range plugins {
		entry, ok := pluginByID(plugin.id)
		if !ok {
			continue
		}
		if entry.multi {
			value, _ := p.propertyValue(filepath.Dir(file), "loom.platform")
			platform := types.Ecosystem(value)
			switch platform {
			case types.EcoFabric, types.EcoForge, types.EcoNeoforge:
				if !slices.Contains(out, platform) {
					out = append(out, platform)
				}
			default:
				p.findings.add(file, plugin.line, "plugin "+plugin.id+" requires platform configuration that was not resolved")
			}
		}
		for _, eco := range entry.ecos {
			if !slices.Contains(out, eco) {
				out = append(out, eco)
			}
		}
	}
	slices.Sort(out)
	return out
}

func pluginByID(id string) (struct {
	id    string
	ecos  []types.Ecosystem
	multi bool
}, bool,
) {
	for _, entry := range pluginEcosystems {
		if entry.id == id {
			return entry, true
		}
	}
	return pluginEcosystems[0], false
}

// appliedPlugins returns every plugin a script applies, resolving convention
// plugins from buildSrc and composite builds.
func (p *prober) appliedPlugins(file, content string, depth int) []appliedPlugin {
	var out []appliedPlugin
	seen := map[string]bool{}
	mask := stringMask(content)

	body, bodyStart, ok := blockNamed(content, "plugins")
	if ok {
		cursor := bodyStart
		for _, item := range splitEntries(body) {
			if item == "" || applyFalsePattern.MatchString(item) {
				continue
			}
			rel := strings.Index(content[cursor:], item)
			if rel < 0 {
				continue
			}
			line := lineAt(content, cursor+rel)
			cursor += rel + len(item)
			id, rest, found := pluginID(item)
			if found {
				if !applyFalsePattern.MatchString(rest) {
					p.appendPlugin(file, line, id, depth, seen, &out)
				}
			}
			for _, match := range aliasPattern.FindAllStringSubmatchIndex(item, -1) {
				catalog := item[match[2]:match[3]]
				key := strings.ReplaceAll(item[match[4]:match[5]], ".", "-")
				if catalog != "libs" {
					p.findings.add(file, line, "plugin alias "+catalog+".plugins."+key+" uses a catalog that was not resolved")
					continue
				}
				id, ok := p.catalogPluginID(key)
				if !ok {
					p.findings.add(file, line, "plugin alias libs.plugins."+key+" could not be resolved to a plugin id")
					continue
				}
				p.appendPlugin(file, line, id, depth, seen, &out)
			}
		}
	}

	for _, match := range applyPluginPattern.FindAllStringIndex(content, -1) {
		if inLiteral(mask, match[0]) {
			continue
		}
		line := lineAt(content, match[0])
		id, _, ok := readStringLiteral(content[match[1]:])
		if !ok {
			p.findings.add(file, line,
				"apply plugin names a plugin class or expression instead of a literal plugin id; it was not resolved")
			continue
		}
		p.appendPlugin(file, line, id, depth, seen, &out)
	}
	return out
}

// scriptSource is a build script contributing configuration to a project.
type scriptSource struct {
	file    string
	content string
}

// appliedScriptSources returns the convention plugin scripts a build script
// applies, so their configuration counts as part of the project.
func (p *prober) appliedScriptSources(file string, plugins []appliedPlugin) []scriptSource {
	seen := map[string]bool{file: true}
	var out []scriptSource
	for _, plugin := range plugins {
		if seen[plugin.source] {
			continue
		}
		seen[plugin.source] = true
		convention, err := p.readFile(plugin.source)
		if err != nil {
			continue
		}
		out = append(out, scriptSource{file: plugin.source, content: stripComments(convention)})
	}
	return out
}

// javaConfigOf merges the Java configuration of convention scripts and the
// project script itself; the project script wins.
func (p *prober) javaConfigOf(projectFile, content, buildDir string, sources []scriptSource) JavaRequirements {
	var req JavaRequirements
	for _, source := range append(sources, scriptSource{file: projectFile, content: content}) {
		block := p.javaConfig(source.file, source.content, buildDir)
		if block.Toolchain != nil {
			req.Toolchain = block.Toolchain
		}
		if block.Release != nil {
			req.Release = block.Release
		}
		if block.Source != nil {
			req.Source = block.Source
		}
		if block.Target != nil {
			req.Target = block.Target
		}
	}
	return req
}

func (p *prober) appendPlugin(file string, line int, id string, depth int, seen map[string]bool, out *[]appliedPlugin) {
	if seen[id] {
		return
	}
	seen[id] = true
	*out = append(*out, appliedPlugin{id: id, line: line, source: file})

	if depth >= maxConventionDepth {
		return
	}
	convention, ok := p.conventionScript(id)
	if !ok {
		return
	}
	content, err := p.readFile(convention)
	if err != nil {
		return
	}
	for _, nested := range p.appliedPlugins(convention, stripComments(content), depth+1) {
		if seen[nested.id] {
			continue
		}
		seen[nested.id] = true
		*out = append(*out, appliedPlugin{id: nested.id, line: line, source: convention})
	}
}

const maxConventionDepth = 8

func splitEntries(body string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '\'', '"':
			end := skipString(body, i)
			if end < 0 {
				return out
			}
			i = end
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '\n', ';':
			if depth == 0 {
				out = append(out, strings.TrimSpace(body[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(body[start:]))
}

func (p *prober) indexConventions() {
	for _, build := range p.builds {
		for _, source := range conventionSourceDirs(build.Dir) {
			p.indexConventionDir(source)
		}
	}
}

func (p *prober) indexConventionDir(source string) {
	entries, err := os.ReadDir(source)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(source, entry.Name())
		if !p.contains(path) {
			continue
		}
		id := strings.TrimSuffix(strings.TrimSuffix(entry.Name(), ".gradle.kts"), ".gradle")
		if id == entry.Name() || id == "" {
			continue
		}
		if _, exists := p.conventions[id]; !exists {
			p.conventions[id] = path
		}
	}
}

func conventionSourceDirs(dir string) []string {
	var out []string
	for _, base := range []string{"buildSrc", "build-logic", "buildlogic"} {
		root := filepath.Join(dir, base)
		for _, src := range []string{
			filepath.Join(root, "src", "main", "kotlin"),
			filepath.Join(root, "src", "main", "groovy"),
		} {
			if info, err := os.Stat(src); err == nil && info.IsDir() {
				out = append(out, src)
			}
		}
	}
	return out
}

func (p *prober) conventionScript(id string) (string, bool) {
	path, ok := p.conventions[id]
	return path, ok
}

func (p *prober) catalogPluginID(alias string) (string, bool) {
	for _, key := range []string{alias, alias + "-plugin"} {
		if id, ok := p.catalog[key]; ok {
			return id, true
		}
	}
	return "", false
}

func (p *prober) propertyValue(dir, key string) (string, bool) {
	for {
		if value, ok := p.propertiesOf(dir)[key]; ok {
			return value, true
		}
		if dir == p.root || slices.ContainsFunc(p.builds, func(build GradleBuild) bool { return build.Dir == dir }) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir || !p.contains(parent) {
			break
		}
		dir = parent
	}
	return "", false
}

func (p *prober) propertiesOf(dir string) map[string]string {
	if cached, ok := p.props[dir]; ok {
		return cached
	}
	path := p.firstExisting(dir, "gradle.properties")
	values := map[string]string{}
	if path != "" {
		if content, ok := p.readOptional(path); ok {
			parsed, err := ParseProperties(content)
			if err != nil {
				p.findings.add(path, 0, "Gradle properties could not be decoded: "+err.Error())
			} else {
				values = parsed
			}
		}
	}
	p.props[dir] = values
	return values
}

func (p *prober) javaConfig(file, content, dir string) JavaRequirements {
	var req JavaRequirements
	mask := stringMask(content)
	for _, match := range javaVersionPattern.FindAllStringSubmatchIndex(content, -1) {
		if inLiteral(mask, match[0]) {
			continue
		}
		line := lineAt(content, match[0])
		if req.Toolchain != nil {
			continue
		}
		major, ok := p.resolveInt(file, dir, content[match[2]:match[3]], line)
		if !ok {
			continue
		}
		req.Toolchain = &JavaConstraint{Major: major}
	}
	req.Release = p.releaseFlag(file, content, dir, "release")
	req.Source = p.releaseFlag(file, content, dir, "sourceCompatibility")
	req.Target = p.releaseFlag(file, content, dir, "targetCompatibility")
	return req
}

var javaVersionPattern = regexp.MustCompile(`JavaLanguageVersion\s*\.\s*of\s*\(([^)]*)\)`)

// releaseFlag resolves a javac level flag such as options.release,
// sourceCompatibility or targetCompatibility.
func (p *prober) releaseFlag(file, content, dir, name string) *int {
	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
	mask := stringMask(content)
	for _, loc := range pattern.FindAllStringIndex(content, -1) {
		if inLiteral(mask, loc[0]) {
			continue
		}
		expr, ok := argumentAfter(content, loc[1])
		if !ok {
			continue
		}
		if value, ok := p.resolveInt(file, dir, expr, lineAt(content, loc[0])); ok {
			return &value
		}
	}
	return nil
}

// argumentAfter reads a value written as "name = <token>", "name(...)" or a
// dotted call chain such as "name.set(...)" following the byte offset from.
func argumentAfter(content string, from int) (string, bool) {
	index := from
	for range 8 {
		for index < len(content) && isSpace(content[index]) {
			index++
		}
		if index >= len(content) {
			return "", false
		}
		switch {
		case content[index] == '=' && (index+1 >= len(content) || content[index+1] != '='):
			return firstToken(content[index+1:]), true
		case content[index] == '.':
			index++
		case isIdentifierByte(content[index]):
			for index < len(content) && isIdentifierByte(content[index]) {
				index++
			}
			for index < len(content) && isSpace(content[index]) {
				index++
			}
			if index >= len(content) || content[index] != '(' {
				continue
			}
			body, ok := balancedParens(content, index)
			if !ok {
				return "", false
			}
			return strings.TrimSpace(body), true
		default:
			return "", false
		}
	}
	return "", false
}

func isIdentifierByte(char byte) bool {
	return char == '_' || char == '$' ||
		(char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
}

func firstToken(text string) string {
	text = strings.TrimSpace(text)
	if idx := strings.IndexAny(text, " \t\r\n,;)}"); idx >= 0 {
		return text[:idx]
	}
	return text
}

// resolveInt resolves a Java level written as a literal, a Gradle property
// reference or a catalog-free identifier.
func (p *prober) resolveInt(file, dir, expr string, line int) (int, bool) {
	expr = strings.TrimSpace(expr)
	// Unwrap accessors such as JavaLanguageVersion.of(x).asInt().
	for range 4 {
		open := strings.Index(expr, "(")
		if open < 0 || !strings.HasSuffix(expr, ")") {
			break
		}
		body, ok := balancedParens(expr, open)
		if !ok {
			break
		}
		expr = strings.TrimSpace(body)
	}
	expr = strings.TrimSpace(strings.TrimPrefix(expr, "project."))
	expr = strings.TrimSpace(strings.TrimPrefix(expr, "rootProject."))
	if value, ok := literalString(expr); ok {
		expr = value
	}
	if major, err := strconv.Atoi(expr); err == nil {
		return major, true
	}
	if version, ok := strings.CutPrefix(expr, "JavaVersion.VERSION_"); ok {
		if major, err := strconv.Atoi(version); err == nil {
			return major, true
		}
	}
	if value, ok := p.propertyValue(dir, expr); ok {
		if major, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			return major, true
		}
		p.findings.add(file, line, "Gradle property "+expr+"="+value+" is not a Java version")
		return 0, false
	}
	p.findings.add(file, line, "Java version "+expr+" could not be resolved statically")
	return 0, false
}
