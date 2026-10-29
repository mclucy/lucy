// Command genwiki renders the documentation site as GitHub wiki pages.
//
// GitHub wikis store every page as a flat Markdown file in one Git repository.
// The site navigation defines which pages exist and in what order; the page
// title defines the file name. Links between site pages are rewritten to the
// flat wiki page names.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	homeSlug = "Home"
	docsSite = "https://lucy.lc/docs"
	docsRepo = "https://github.com/mclucy/lucy/tree/main/docs"
)

type siteConfig struct {
	Navigation struct {
		Pages []json.RawMessage `json:"pages"`
	} `json:"navigation"`
}

type section struct {
	Name  string
	Paths []string
}

type page struct {
	Path        string
	Title       string
	Description string
	Slug        string
	Body        string
}

func main() {
	docsDir := flag.String("docs", "docs", "path to the documentation site directory")
	outDir := flag.String("out", "dist/wiki", "output directory for wiki pages")
	flag.Parse()

	if err := run(*docsDir, *outDir); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] generate wiki: %v\n", err)
		os.Exit(1)
	}
}

func run(docsDir, outDir string) error {
	sections, err := readNavigation(filepath.Join(docsDir, "docs.json"))
	if err != nil {
		return err
	}

	pages, err := readPages(docsDir, sections)
	if err != nil {
		return err
	}

	links := make(map[string]string, len(pages))
	for _, p := range pages {
		links[p.Path] = p.Slug
	}
	for i := range pages {
		body, err := render(pages[i].Body, links)
		if err != nil {
			return fmt.Errorf("%s: %w", pages[i].Path, err)
		}
		pages[i].Body = body
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := removeStalePages(outDir); err != nil {
		return err
	}

	for _, p := range pages {
		if err := os.WriteFile(filepath.Join(outDir, p.Slug+".md"), []byte(renderPage(p)), 0o644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(outDir, "_Sidebar.md"), []byte(renderSidebar(sections, pages)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "_Footer.md"), []byte(renderFooter()), 0o644); err != nil {
		return err
	}
	return nil
}

func readNavigation(path string) ([]section, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg siteConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	sections := []section{{}}
	for _, raw := range cfg.Navigation.Pages {
		var pagePath string
		if err := json.Unmarshal(raw, &pagePath); err == nil {
			sections[0].Paths = append(sections[0].Paths, pagePath)
			continue
		}

		var group struct {
			Group string   `json:"group"`
			Pages []string `json:"pages"`
		}
		if err := json.Unmarshal(raw, &group); err != nil {
			return nil, fmt.Errorf("parse %s navigation entry: %w", path, err)
		}
		sections = append(sections, section{
			Name:  group.Group,
			Paths: group.Pages,
		})
	}
	return sections, nil
}

func readPages(docsDir string, sections []section) ([]page, error) {
	var paths []string
	for _, s := range sections {
		paths = append(paths, s.Paths...)
	}
	if len(paths) == 0 {
		return nil, errors.New("docs.json defines no pages")
	}

	pages := make([]page, 0, len(paths))
	slugs := make(map[string]string, len(paths))
	for _, p := range paths {
		data, err := os.ReadFile(filepath.Join(docsDir, p+".mdx"))
		if err != nil {
			return nil, fmt.Errorf("read page %q: %w", p, err)
		}
		title, description, body, err := parseFrontmatter(string(data))
		if err != nil {
			return nil, fmt.Errorf("page %q: %w", p, err)
		}
		if title == "" {
			return nil, fmt.Errorf("page %q has no title", p)
		}

		slug := homeSlug
		if p != "index" {
			slug = slugify(title)
		}
		if slug == "" {
			return nil, fmt.Errorf("page %q title %q produces an empty file name", p, title)
		}
		if other, ok := slugs[slug]; ok {
			return nil, fmt.Errorf("pages %q and %q both map to %s.md", other, p, slug)
		}
		slugs[slug] = p

		pages = append(pages, page{
			Path:        p,
			Title:       title,
			Description: description,
			Slug:        slug,
			Body:        body,
		})
	}
	return pages, nil
}

func parseFrontmatter(src string) (title, description, body string, err error) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	if !strings.HasPrefix(src, "---\n") {
		return "", "", "", errors.New("missing frontmatter")
	}
	rest := src[len("---\n"):]
	index := strings.Index(rest, "\n---\n")
	if index < 0 {
		return "", "", "", errors.New("unterminated frontmatter")
	}

	for _, line := range strings.Split(rest[:index], "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		switch strings.TrimSpace(key) {
		case "title":
			title = value
		case "description":
			description = value
		}
	}

	return title, description, strings.TrimLeft(rest[index+len("\n---\n"):], "\n"), nil
}

var slugSeparators = regexp.MustCompile(`[^A-Za-z0-9]+`)

func slugify(title string) string {
	return strings.Trim(slugSeparators.ReplaceAllString(title, "-"), "-")
}

func renderPage(p page) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", p.Title)
	if p.Description != "" {
		fmt.Fprintf(&b, "\n*%s*\n", p.Description)
	}
	b.WriteString("\n")
	b.WriteString(strings.TrimRight(p.Body, "\n"))
	b.WriteString("\n")
	return b.String()
}

func renderSidebar(sections []section, pages []page) string {
	byPath := make(map[string]page, len(pages))
	for _, p := range pages {
		byPath[p.Path] = p
	}

	var b strings.Builder
	b.WriteString("<!-- Generated by tools/genwiki from docs/docs.json. Do not edit this wiki by hand. -->\n")
	for _, s := range sections {
		if len(s.Paths) == 0 {
			continue
		}
		if s.Name != "" {
			fmt.Fprintf(&b, "\n### %s\n\n", s.Name)
		}
		for _, path := range s.Paths {
			p := byPath[path]
			fmt.Fprintf(&b, "- [%s](%s)\n", p.Title, p.Slug)
		}
	}
	return b.String()
}

func renderFooter() string {
	return fmt.Sprintf("---\n\nGenerated from the [`docs/`](%s) directory. [Read the documentation site](%s).\n", docsRepo, docsSite)
}

func removeStalePages(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

var (
	componentOpen  = regexp.MustCompile(`^<(Warning|Note|Tip|Important|Caution)>`)
	componentClose = regexp.MustCompile(`^</(Warning|Note|Tip|Important|Caution)>$`)
	stepOpen       = regexp.MustCompile(`^<Step\s+title="([^"]*)">$`)
	attribute      = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_-]*)(?:="([^"]*)")?`)
	internalLink   = regexp.MustCompile(`\]\((/[^)\s]*)\)`)
)

func render(body string, links map[string]string) (string, error) {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	inFence := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if inFence {
			out = append(out, line)
			if strings.HasPrefix(trimmed, "```") {
				inFence = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			out = append(out, line)
			inFence = true
			continue
		}

		switch {
		case componentOpen.MatchString(trimmed):
			kind := strings.ToUpper(componentOpen.FindStringSubmatch(trimmed)[1])
			if end := strings.Index(trimmed, "</"); end >= 0 {
				inner := strings.TrimSpace(trimmed[len(componentOpen.FindStringSubmatch(trimmed)[0]):end])
				out = appendAlert(out, kind, []string{inner})
				continue
			}
			content, next, err := collectComponent(lines, i+1, "</"+componentOpen.FindStringSubmatch(trimmed)[1]+">")
			if err != nil {
				return "", err
			}
			out = appendAlert(out, kind, content)
			i = next
		case trimmed == "<Steps>" || trimmed == "</Steps>" || trimmed == "</Step>":
			continue
		case stepOpen.MatchString(trimmed):
			title := stepOpen.FindStringSubmatch(trimmed)[1]
			content, next, err := collectComponent(lines, i+1, "</Step>")
			if err != nil {
				return "", err
			}
			out = append(out, "", "### "+title, "")
			out = append(out, dedent(trimBlank(content))...)
			out = append(out, "")
			i = next
		case strings.HasPrefix(trimmed, "<ParamField "):
			attrs := parseAttributes(strings.TrimSuffix(strings.TrimSuffix(trimmed, ">"), "/"))
			content, next, err := collectComponent(lines, i+1, "</ParamField>")
			if err != nil {
				return "", err
			}
			out = append(out, "", renderParamField(attrs), "")
			out = append(out, dedent(trimBlank(content))...)
			out = append(out, "")
			i = next
		case strings.HasPrefix(trimmed, "<GitHub.Repo "):
			repo := parseAttributes(trimmed)["repo"]
			if repo == "" {
				return "", fmt.Errorf("GitHub.Repo without repo at line %d", i+1)
			}
			out = append(out, "", fmt.Sprintf("[github.com/%s](https://github.com/%s)", repo, repo), "")
		default:
			if componentClose.MatchString(trimmed) {
				return "", fmt.Errorf("unexpected %s at line %d", trimmed, i+1)
			}
			out = append(out, line)
		}
	}

	text := strings.TrimLeft(strings.Join(out, "\n"), "\n")
	text = normalizeBlankLines(text)
	text = strings.TrimRight(text, "\n") + "\n"
	return rewriteLinks(text, links)
}

func normalizeBlankLines(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	blank := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			blank = false
			out = append(out, line)
			continue
		}
		if !inFence && trimmed == "" {
			if blank {
				continue
			}
			blank = true
			out = append(out, "")
			continue
		}
		blank = false
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func collectComponent(lines []string, start int, closeTag string) ([]string, int, error) {
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == closeTag {
			return lines[start:i], i, nil
		}
	}
	return nil, 0, fmt.Errorf("missing %s", closeTag)
}

func appendAlert(out []string, kind string, content []string) []string {
	content = dedent(trimBlank(content))
	out = append(out, "", "> [!"+kind+"]")
	for _, line := range content {
		if line == "" {
			out = append(out, ">")
			continue
		}
		out = append(out, "> "+line)
	}
	return append(out, "")
}

func renderParamField(attrs map[string]string) string {
	label := attrs["query"]
	if label == "" {
		label = attrs["path"]
	}
	parts := make([]string, 0, 3)
	if kind := attrs["type"]; kind != "" {
		parts = append(parts, kind)
	}
	if _, ok := attrs["required"]; ok {
		parts = append(parts, "required")
	}
	if value, ok := attrs["default"]; ok {
		parts = append(parts, "default: `"+value+"`")
	}

	line := "**`" + label + "`**"
	if len(parts) > 0 {
		line += " (" + strings.Join(parts, ", ") + ")"
	}
	return line
}

func parseAttributes(s string) map[string]string {
	attrs := make(map[string]string)
	for _, match := range attribute.FindAllStringSubmatch(s, -1) {
		attrs[match[1]] = match[2]
	}
	return attrs
}

func dedent(lines []string) []string {
	indent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		width := len(line) - len(strings.TrimLeft(line, " "))
		if indent < 0 || width < indent {
			indent = width
		}
	}
	if indent <= 0 {
		return lines
	}

	result := make([]string, len(lines))
	for i, line := range lines {
		if len(line) >= indent {
			result[i] = line[indent:]
			continue
		}
		result[i] = strings.TrimLeft(line, " ")
	}
	return result
}

func trimBlank(lines []string) []string {
	lines = slices.Clone(lines)
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func rewriteLinks(text string, links map[string]string) (string, error) {
	lines := strings.Split(text, "\n")
	inFence := false
	var firstErr error

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if inFence {
			if strings.HasPrefix(trimmed, "```") {
				inFence = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			inFence = true
			continue
		}
		if strings.Contains(line, "(") {
			lines[i] = rewriteLineLinks(line, links, &firstErr)
		}
	}
	if firstErr != nil {
		return "", firstErr
	}
	return strings.Join(lines, "\n"), nil
}

func rewriteLineLinks(line string, links map[string]string, firstErr *error) string {
	return internalLink.ReplaceAllStringFunc(line, func(match string) string {
		target := strings.TrimSuffix(strings.TrimPrefix(match, "]("), ")")
		path, anchor, hasAnchor := strings.Cut(target, "#")
		key := strings.TrimPrefix(path, "/")
		if key == "" {
			key = "index"
		}
		slug, ok := links[key]
		if !ok {
			if *firstErr == nil {
				*firstErr = fmt.Errorf("link target %q has no page in docs.json", target)
			}
			return match
		}
		if hasAnchor {
			return "](" + slug + "#" + anchor + ")"
		}
		return "](" + slug + ")"
	})
}
