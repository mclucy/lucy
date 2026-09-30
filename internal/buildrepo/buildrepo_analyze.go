package buildrepo

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/mclucy/lucy/types"
)

func (p *prober) analyzeBuilds() error {
	for i := range p.builds {
		build := &p.builds[i]
		build.IncludedBuilds = p.includedBuildDirs(build.Dir, build.SettingsFile)
		build.Wrapper = p.parseWrapper(build.Dir)
		for dir := filepath.Dir(build.Dir); build.Wrapper == nil && p.contains(dir); dir = filepath.Dir(dir) {
			build.Wrapper = p.parseWrapper(dir)
			if dir == p.root {
				break
			}
		}
		p.applyDaemonSettings(build)
		if err := p.analyzeProjects(build); err != nil {
			return fmt.Errorf("buildrepo: probe %s: %w", build.Dir, err)
		}
	}
	return nil
}

func (p *prober) applyDaemonSettings(build *GradleBuild) {
	if home, ok := p.propertiesOf(build.Dir)["org.gradle.java.home"]; ok && home != "" {
		build.JavaHome = home
		if !filepath.IsAbs(home) {
			build.JavaHome = filepath.Join(build.Dir, filepath.FromSlash(home))
		}
	}
	criteria := p.firstExisting(build.Dir, filepath.Join("gradle", "gradle-daemon-jvm.properties"))
	if criteria == "" {
		return
	}
	content, ok := p.readOptional(criteria)
	if !ok {
		p.findings.add(criteria, 0, "daemon JVM criteria file could not be read")
		return
	}
	properties, err := ParseProperties(content)
	if err != nil {
		p.findings.add(criteria, 0, "daemon JVM criteria could not be decoded: "+err.Error())
		return
	}
	constraint := &JavaConstraint{
		Major: parseMajor(properties["toolchainVersion"]), Vendor: properties["toolchainVendor"],
	}
	if constraint.Major > 0 || constraint.Vendor != "" {
		build.Daemon = constraint
	}
}

var majorPattern = regexp.MustCompile(`\d+`)

func parseMajor(text string) int {
	match := majorPattern.FindString(text)
	if match == "" {
		return 0
	}
	value := 0
	for _, digit := range match {
		value = value*10 + int(digit-'0')
	}
	return value
}

func (p *prober) parseWrapper(dir string) *Wrapper {
	properties := p.firstExisting(dir, filepath.Join("gradle", "wrapper", "gradle-wrapper.properties"))
	scriptName := "gradlew"
	if runtime.GOOS == "windows" {
		scriptName = "gradlew.bat"
	}
	script := p.firstExisting(dir, scriptName)
	jar := p.firstExisting(dir, filepath.Join("gradle", "wrapper", "gradle-wrapper.jar"))
	if properties == "" && script == "" && jar == "" {
		return nil
	}
	wrapper := &Wrapper{Script: script, Jar: jar, Properties: properties}
	if properties == "" {
		p.findings.add(script, 0, "gradlew is present without gradle/wrapper/gradle-wrapper.properties; no Gradle version is pinned")
		return wrapper
	}
	content, ok := p.readOptional(properties)
	if !ok {
		p.findings.add(properties, 0, "wrapper properties could not be read; no Gradle version is pinned")
		return wrapper
	}
	values, err := ParseProperties(content)
	if err != nil {
		p.findings.add(properties, 0, "wrapper properties could not be decoded: "+err.Error())
		return wrapper
	}
	wrapper.DistributionURL = values["distributionUrl"]
	wrapper.DistributionSHA256 = values["distributionSha256Sum"]
	wrapper.Version = gradleVersionFromURL(wrapper.DistributionURL)
	switch {
	case wrapper.DistributionURL == "":
		p.findings.add(properties, 0, "wrapper properties declare no distributionUrl; the pinned Gradle version is unknown")
	case wrapper.Version == "":
		p.findings.add(properties, 0, "wrapper distributionUrl "+wrapper.DistributionURL+" does not name a Gradle version")
	}
	if wrapper.DistributionURL != "" && wrapper.DistributionSHA256 == "" {
		p.findings.add(properties, 0, "wrapper distribution is not pinned by a distributionSha256Sum checksum")
	}
	if jar == "" {
		p.findings.add(properties, 0, "wrapper jar gradle/wrapper/gradle-wrapper.jar is missing; the checked in wrapper cannot run")
	}
	return wrapper
}

var gradleURLPattern = regexp.MustCompile(`gradle-([0-9][0-9A-Za-z.\-]*?)-(?:bin|all)\.zip`)

func gradleVersionFromURL(url string) string {
	match := gradleURLPattern.FindStringSubmatch(url)
	if match == nil {
		return ""
	}
	return match[1]
}

func (p *prober) analyzeProjects(build *GradleBuild) error {
	paths := p.declaredProjects(build.Dir, build.SettingsFile)

	order := make([]string, 0, len(paths))
	for path := range paths {
		order = append(order, path)
	}
	slices.Sort(order)

	var broadcast JavaRequirements
	var broadcastSet bool
	if build.BuildFile != "" {
		if content, ok := p.readOptional(build.BuildFile); ok {
			content = stripComments(content)

			broadcast, broadcastSet = p.broadcastJava(build, content)
		}
	}

	for _, path := range order {
		dir := paths[path]
		if dir == "" {
			build.Projects = append(build.Projects, Project{Path: path})
			continue
		}
		if !p.contains(dir) {
			p.findings.add(build.SettingsFile, 0, "project "+path+" resolves outside the repository root and was not read")
			continue
		}
		project := Project{
			Path:      path,
			Dir:       dir,
			BuildFile: p.firstExisting(dir, "build.gradle", "build.gradle.kts"),
		}
		if project.BuildFile != "" {
			if content, ok := p.readOptional(project.BuildFile); ok {
				content = stripComments(content)
				plugins := p.appliedPlugins(project.BuildFile, content, 0)
				sources := p.appliedScriptSources(project.BuildFile, plugins)
				project.Ecosystems = p.scriptEcosystems(project.BuildFile, plugins)
				project.Java = p.javaConfigOf(project.BuildFile, content, build.Dir, sources)

				p.noteGeneratedSources(&project)
				p.noteConditional(project.BuildFile, content)
			} else {
				p.findings.add(project.BuildFile, 0, "project build script could not be read")
			}
		}
		p.corroborate(&project)
		p.inheritJava(&project, broadcast, broadcastSet)
		build.Projects = append(build.Projects, project)
		if err := p.ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

// broadcastJava returns the Java configuration a root script applies to every
// project through allprojects or subprojects blocks.
func (p *prober) broadcastJava(build *GradleBuild, content string) (JavaRequirements, bool) {
	var req JavaRequirements
	found := false
	for _, name := range []string{"allprojects", "subprojects"} {
		body, _, ok := blockNamed(content, name)
		if !ok {
			continue
		}
		block := p.javaConfig(build.BuildFile, body, build.Dir)
		if block.Toolchain != nil && req.Toolchain == nil {
			req.Toolchain = block.Toolchain
			found = true
		}
		if block.Release != nil && req.Release == nil {
			req.Release = block.Release
			found = true
		}
	}
	return req, found
}

func (p *prober) inheritJava(project *Project, broadcast JavaRequirements, broadcastSet bool) {
	if broadcastSet {
		if project.Java.Toolchain == nil {
			project.Java.Toolchain = broadcast.Toolchain
		}
		if project.Java.Release == nil {
			project.Java.Release = broadcast.Release
		}
	}
	if project.Java.Toolchain == nil && project.Java.Release == nil &&
		project.Java.Source == nil && project.Java.Target == nil {
		p.findings.add(project.BuildFile, 0, "Java requirements of this project are unknown")
	}
}

var modMetadata = []struct {
	file string
	eco  types.Ecosystem
}{
	{file: filepath.Join("src", "main", "resources", "fabric.mod.json"), eco: types.EcoFabric},

	{file: filepath.Join("src", "main", "resources", "META-INF", "mods.toml"), eco: types.EcoForge},
	{file: filepath.Join("src", "main", "resources", "META-INF", "neoforge.mods.toml"), eco: types.EcoNeoforge},
}

// corroborate fills ecosystems from loader metadata when no modding plugin was
// found. Metadata only ever confirms; it never overrides a plugin.
func (p *prober) corroborate(project *Project) {
	if len(project.Ecosystems) > 0 {
		return
	}
	var found []string
	for _, entry := range modMetadata {
		path := filepath.Join(project.Dir, entry.file)
		if _, err := os.Stat(path); err == nil && p.contains(path) {
			found = append(found, entry.file)
			if !slices.Contains(project.Ecosystems, entry.eco) {
				project.Ecosystems = append(project.Ecosystems, entry.eco)
			}
		}
	}
	if len(found) > 0 {
		p.findings.add(project.BuildFile, 0,
			"no modding plugin is applied; ecosystem taken from "+strings.Join(found, ", ")+" metadata")
	}
	slices.Sort(project.Ecosystems)
}

func (p *prober) noteGeneratedSources(project *Project) {
	for _, name := range []string{"generated", "generated-src", "chiseledSrc"} {
		path := filepath.Join(project.Dir, "src", name)
		if _, err := os.Stat(path); err == nil && p.contains(path) {
			p.findings.add(project.BuildFile, 0,
				"generated sources under "+filepath.ToSlash(filepath.Join("src", name))+" are not inspected")
		}
	}
}

var conditionalPattern = regexp.MustCompile(`(?m)^\s*(if\s*\(|if\s+|when\s*\(|switch\s*\(|\}\s*else\b)`)

func (p *prober) noteConditional(file, content string) {
	if match := conditionalPattern.FindStringIndex(content); match != nil {
		p.findings.add(file, lineAt(content, match[0]),
			"script contains conditional configuration; literal declarations do not establish which branches are active")
	}
}
