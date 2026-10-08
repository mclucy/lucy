// Package buildrepo statically probes the Gradle build layout of a Minecraft
// mod source repository. Gradle is never executed.
//
// Contract: filesystem paths are absolute, Gradle paths use the colon form
// (":" for the root project), and nothing outside the probed root is read.
// Whatever cannot be read from literals alone is reported as a Finding rather
// than guessed; a cancelled context or an unreadable directory fails Probe
// instead.
package buildrepo

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/types"
)

type Layout struct {
	Root     string
	Builds   []GradleBuild
	Findings []Finding
}

type GradleBuild struct {
	Dir            string
	SettingsFile   string
	BuildFile      string
	Wrapper        *Wrapper
	Projects       []Project
	IncludedBuilds []string
	Daemon         *JavaConstraint
	JavaHome       string
}

type Project struct {
	Path       string
	Dir        string
	BuildFile  string
	Ecosystems []types.Ecosystem
	Java       JavaRequirements
}

type Wrapper struct {
	Script, Jar, Properties             string
	DistributionURL, DistributionSHA256 string
	Version                             string
}

type JavaConstraint struct {
	Major  int
	Vendor string
}

// JavaRequirements separates compiler selection from bytecode compatibility.
// Nil fields denote absent or unresolved declarations, not unrestricted compilation.
type JavaRequirements struct {
	Toolchain               *JavaConstraint
	Release, Source, Target *int
}

type Finding struct {
	File    string
	Line    int
	Message string
}

// Probe reports literal declarations; evaluating Gradle is required to establish the active build graph.
func Probe(ctx context.Context, root string) (layout Layout, err error) {
	if err := ctx.Err(); err != nil {
		return Layout{}, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Layout{}, fmt.Errorf("buildrepo: resolve %s: %w", root, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Layout{}, fmt.Errorf("buildrepo: stat %s: %w", abs, err)
	}
	if !info.IsDir() {
		return Layout{}, fmt.Errorf("buildrepo: %s is not a directory", abs)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return Layout{}, fmt.Errorf("buildrepo: resolve symlinks of %s: %w", abs, err)
	}
	scoped, err := os.OpenRoot(real)
	if err != nil {
		return Layout{}, fmt.Errorf("open repository root: %w", err)
	}
	defer func() {
		if closeErr := scoped.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close repository root: %w", closeErr))
		}
	}()

	p := &prober{
		ctx:  ctx,
		root: real, scoped: scoped,
		props:              map[string]map[string]string{},
		catalog:            map[string]string{},
		catalogs:           map[string]map[string]string{},
		conventions:        map[string]string{},
		conventionSettings: map[string]string{},
	}
	if err := p.discoverBuilds(); err != nil {
		return Layout{}, err
	}
	p.indexCatalogs()
	p.indexConventions()
	if err := p.analyzeBuilds(); err != nil {
		return Layout{}, err
	}
	for i := range p.builds {
		sortBuild(&p.builds[i])
	}
	return Layout{Root: real, Builds: p.builds, Findings: p.findings.sorted()}, nil
}

// skippedDirs never contain hand written Gradle configuration.
var skippedDirs = map[string]bool{
	".git": true, ".gradle": true, ".idea": true, ".kotlin": true,
	"build": true, "out": true, "target": true, "node_modules": true,
}

// conventionBuildDirs hold plugin sources, never mod targets.
var conventionBuildDirs = map[string]bool{
	"buildSrc": true, "build-logic": true, "buildlogic": true,
}

type prober struct {
	ctx    context.Context
	root   string
	scoped *os.Root

	builds   []GradleBuild
	findings findingList
	// props caches parsed gradle.properties per directory.
	props map[string]map[string]string
	// catalog maps a version catalog alias to a plugin id.
	catalog map[string]string
	// catalogs caches parsed version catalogs per path.
	catalogs map[string]map[string]string
	// conventions maps a convention plugin id to its precompiled script.
	conventions map[string]string
	// conventionSettings holds convention plugin ids declared for settings scripts.
	conventionSettings map[string]string
}

type findingList []Finding

// add records a finding once; the same observation can be reached twice, for
// example when a settings script is read during discovery and analysis.
func (f *findingList) add(file string, line int, msg string) {
	for _, existing := range *f {
		if existing.File == file && existing.Line == line && existing.Message == msg {
			return
		}
	}
	*f = append(*f, Finding{File: file, Line: line, Message: msg})
}

func (f findingList) sorted() []Finding {
	slices.SortStableFunc(f, func(a, b Finding) int {
		return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Message, b.Message))
	})
	return f
}

// contains reports whether target stays inside the root once symlinks are
// resolved. Symlinked entries escaping the root are never followed.
func (p *prober) contains(target string) bool {
	real, err := filepath.EvalSymlinks(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(p.root, real)
	if err != nil {
		return false
	}
	return rel == "." || filepath.IsLocal(rel)
}

func (p *prober) readFile(path string) (string, error) {
	relative, err := filepath.Rel(p.root, path)
	if err != nil || !filepath.IsLocal(relative) {
		return "", errors.New("outside repository root")
	}
	info, err := p.scoped.Stat(relative)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	data, err := p.scoped.ReadFile(relative)
	return string(data), err
}

func (p *prober) readOptional(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	data, err := p.readFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			p.findings.add(path, 0, "file could not be read: "+err.Error())
		}
		return "", false
	}
	return data, true
}

// firstExisting returns the first existing path among candidates.
func (p *prober) firstExisting(dir string, names ...string) string {
	for _, name := range names {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return ""
}

func lineAt(content string, offset int) int {
	return strings.Count(content[:offset], "\n") + 1
}
