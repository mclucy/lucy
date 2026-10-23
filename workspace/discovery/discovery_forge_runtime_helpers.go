package discovery

import (
	"archive/zip"
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/log"
	versionpkg "github.com/mclucy/lucy/version"
)

var forgeRuntimeVersionDirPattern = regexp.MustCompile(
	`^(\d+\.\d+(?:\.\d+)?)-(\d+(?:\.\d+)+)$`,
)

var forgeJarNameVersionPattern = regexp.MustCompile(
	`^forge-(\d+\.\d+(?:\.\d+)?)-(\d+(?:\.\d+)+)(?:-[a-z]+)?\.jar$`,
)

func parseForgeManifest(
	zipReader *zip.Reader,
) (forgeVersion versionpkg.Version, gameVersion versionpkg.Version) {
	for _, f := range zipReader.File {
		if f.Name != "META-INF/MANIFEST.MF" {
			continue
		}

		r, err := f.Open()
		if err != nil {
			continue
		}
		defer fn.CloseReader(r, log.Warn)

		var inForgeSection bool
		var pendingImplVersion string
		s := bufio.NewScanner(r)
		for s.Scan() {
			line := s.Text()
			switch {
			case strings.HasPrefix(line, "Name: "):
				inForgeSection = false
			case line == "Implementation-Title: net.minecraftforge":
				inForgeSection = true
				if pendingImplVersion != "" {
					forgeVersion = versionpkg.Version(pendingImplVersion)
					pendingImplVersion = ""
				}
			case strings.HasPrefix(line, "Implementation-Version: "):
				if after, found := strings.CutPrefix(
					line,
					"Implementation-Version: ",
				); found {
					switch {
					case inForgeSection:
						forgeVersion = versionpkg.Version(after)
					case !inForgeSection && forgeVersion == "" && pendingImplVersion == "":
						pendingImplVersion = after
					}
				}
			case strings.HasPrefix(line, "Specification-Version: "):
				if after, found := strings.CutPrefix(
					line,
					"Specification-Version: ",
				); found && isMinecraftReleaseVersion(after) {
					gameVersion = versionpkg.Version(after)
				}
			}
		}

		break
	}

	return forgeVersion, gameVersion
}

func isMinecraftReleaseVersion(version string) bool {
	if !strings.HasPrefix(version, "1.") {
		return false
	}
	for _, r := range version {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}

func parseForgeVersionTupleFromPath(
	filePath string,
) (gameVersion versionpkg.Version, forgeVersion versionpkg.Version, ok bool) {
	parts := strings.Split(filepath.ToSlash(filePath), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] != "forge" {
			continue
		}
		match := forgeRuntimeVersionDirPattern.FindStringSubmatch(parts[i+1])
		if match == nil {
			continue
		}
		return versionpkg.Version(match[1]), versionpkg.Version(match[2]), true
	}
	if match := forgeJarNameVersionPattern.FindStringSubmatch(filepath.Base(filePath)); match != nil {
		return versionpkg.Version(match[1]), versionpkg.Version(match[2]), true
	}
	return versionpkg.Unknown, versionpkg.Unknown, false
}

func hasConcreteVersion(version versionpkg.Version) bool {
	return version != "" && !version.IsInvalid() && !version.CanInfer()
}

func compareForgeMajor(version versionpkg.Version, target int) int {
	major, _, _ := strings.Cut(string(version), ".")
	if major == "" {
		return -1
	}
	n, err := strconv.Atoi(major)
	if err != nil {
		return -1
	}
	switch {
	case n < target:
		return -1
	case n > target:
		return 1
	default:
		return 0
	}
}

func forgeHasSibling(filePath string, siblings ...string) bool {
	dir := filepath.Dir(filePath)
	for _, sibling := range siblings {
		if _, err := os.Stat(filepath.Join(dir, sibling)); err == nil {
			return true
		}
	}
	return false
}

func hasForgeSibling(context DetectionContext, primary *DetectionFile, siblings ...string) bool {
	for _, sibling := range siblings {
		if _, ok := context.Sibling(primary, sibling); ok {
			return true
		}
	}
	return false
}

func newForgeRuntimeEvidence(
	filePath string,
	gameVersion versionpkg.Version,
	forgeVersion versionpkg.Version,
) *RuntimeEvidence {
	return &RuntimeEvidence{PrimaryPath: filePath, PrimaryRuntime: &catalog.VersionedRef{
		Ecosystem: catalog.EcosystemForge,
		Name:      "forge",
		Version:   forgeVersion,
	}, RuntimeComponents: []catalog.VersionedRef{
		{
			Ecosystem: catalog.EcosystemForge,
			Name:      "forge",
			Version:   forgeVersion,
		},
		{
			Ecosystem: catalog.EcosystemMinecraft,
			Name:      "minecraft",
			Version:   gameVersion,
		},
	}}
}
