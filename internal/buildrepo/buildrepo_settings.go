package buildrepo

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func (p *prober) discoverBuilds() error {
	roots := make(map[string]bool)
	buildDirectories := make([]string, 0)
	err := filepath.WalkDir(p.root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := p.ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if path != p.root && skippedDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		switch entry.Name() {
		case "settings.gradle", "settings.gradle.kts":
			if p.contains(path) {
				roots[filepath.Dir(path)] = true
			}
		case "build.gradle", "build.gradle.kts":
			if p.contains(path) {
				buildDirectories = append(buildDirectories, filepath.Dir(path))
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("buildrepo: scan %s: %w", p.root, err)
	}
	settingsDirectories := slices.Sorted(maps.Keys(roots))
	for _, dir := range buildDirectories {
		covered := false
		for _, settingsDir := range settingsDirectories {
			relative, err := filepath.Rel(settingsDir, dir)
			if err == nil && filepath.IsLocal(relative) {
				covered = true
				break
			}
		}
		if !covered || filepath.Base(dir) == "buildSrc" {
			roots[dir] = true
		}
	}
	pending := slices.Collect(maps.Keys(roots))
	for len(pending) > 0 {
		dir := pending[0]
		pending = pending[1:]
		if err := p.ctx.Err(); err != nil {
			return err
		}
		settings := p.firstExisting(dir, "settings.gradle", "settings.gradle.kts")
		for _, included := range p.includedBuildDirs(dir, settings) {
			if !roots[included] {
				roots[included] = true
				pending = append(pending, included)
			}
		}
	}
	directories := slices.Sorted(maps.Keys(roots))
	for _, dir := range directories {
		p.builds = append(p.builds, GradleBuild{
			Dir: dir, SettingsFile: p.firstExisting(dir, "settings.gradle", "settings.gradle.kts"),
			BuildFile: p.firstExisting(dir, "build.gradle", "build.gradle.kts"),
		})
	}
	return nil
}

func (p *prober) includedBuildDirs(dir, settings string) []string {
	if settings == "" {
		return nil
	}
	content, _ := p.readOptional(settings)
	content = stripComments(content)
	p.noteConditional(settings, content)
	included := make([]string, 0)
	for _, call := range slices.Concat(calls(content, "includeBuild"), groovyCalls(content, "includeBuild")) {
		arguments, line := callArgs(content, call)
		if len(arguments) == 0 {
			continue
		}
		path, ok := literalString(arguments[0])
		if !ok {
			p.findings.add(settings, line, "includeBuild target is not a literal path; composite build not resolved")
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(path))
		if !p.contains(target) {
			if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
				p.findings.add(settings, line, fmt.Sprintf("includeBuild %q is not present in the checkout", path))
			} else {
				p.findings.add(settings, line, fmt.Sprintf("includeBuild %q resolves outside the repository root", path))
			}
			continue
		}
		if !slices.Contains(included, target) {
			included = append(included, target)
		}
	}
	return included
}

func (p *prober) declaredProjects(dir, settings string) map[string]string {
	projects := map[string]string{":": dir}
	if settings == "" {
		return projects
	}
	content, _ := p.readOptional(settings)
	content = stripComments(content)
	mappings := make(map[string]string)
	renames := make(map[string]string)
	unresolved := make(map[string]bool)
	for _, call := range slices.Concat(calls(content, "include"), groovyCalls(content, "include")) {
		arguments, line := callArgs(content, call)
		for _, argument := range arguments {
			name, ok := literalString(argument)
			if !ok {
				p.findings.add(settings, line, "include argument is not a literal project name; project set is incomplete")
				continue
			}
			path := gradlePathOf(name)
			for path != "" && path != ":" {
				if _, exists := projects[path]; !exists {
					projects[path] = ""
				}
				path, _, _ = strings.CutLast(path, ":")
			}
		}
	}
	for _, call := range slices.Concat(calls(content, "project"), groovyCalls(content, "project")) {
		arguments, line := callArgs(content, call)
		if len(arguments) == 0 {
			continue
		}
		path, ok := literalString(arguments[0])
		if !ok || !strings.HasPrefix(path, ":") {
			continue
		}
		if assignment, ok := assignmentAfter(content, call, "projectDir"); ok {
			value, resolved := unwrapValue(assignment)
			if !resolved {
				p.findings.add(settings, line, "project "+path+" projectDir is not a literal path")
				unresolved[path] = true
				continue
			}
			mappings[path] = value
		}
		if assignment, ok := assignmentAfter(content, call, "name"); ok {
			name, resolved := literalString(assignment)
			if resolved && name != "" && !strings.Contains(name, ":") {
				parent, _, _ := strings.CutLast(path, ":")
				renames[path] = parent + ":" + name
			} else {
				p.findings.add(settings, line, "project "+path+" name is not a literal; final project path is unknown")
			}
		}
	}
	for path := range projects {
		if path == ":" {
			continue
		}
		if unresolved[path] {
			projects[path] = ""
			continue
		}
		physical := strings.ReplaceAll(strings.TrimPrefix(path, ":"), ":", "/")
		if mapped, ok := mappings[path]; ok {
			physical = mapped
		}
		if filepath.IsAbs(physical) {
			projects[path] = filepath.Clean(physical)
		} else {
			projects[path] = filepath.Join(dir, filepath.FromSlash(physical))
		}
	}
	for _, original := range slices.SortedFunc(maps.Keys(renames), func(a, b string) int {
		return len(b) - len(a)
	}) {
		renamed := renames[original]
		if original == renamed {
			continue
		}
		for path, physical := range projects {
			if path == original || strings.HasPrefix(path, original+":") {
				target := renamed + strings.TrimPrefix(path, original)
				if _, exists := projects[target]; exists {
					p.findings.add(settings, 0, "project rename collides with "+target)
					continue
				}
				delete(projects, path)
				projects[target] = physical
			}
		}
	}
	return projects
}

func gradlePathOf(name string) string {
	name = ":" + strings.Trim(strings.TrimSpace(name), ":")
	if name == ":" || strings.Contains(name, "::") {
		return ""
	}
	return name
}
