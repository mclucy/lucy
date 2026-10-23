package discovery

import (
	"archive/zip"
	"bufio"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

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

// parseForgeManifest reads the Forge and Minecraft versions from the
// per-section attributes of a Forge universal jar manifest. The manifest may
// list the implementation version before the implementation title, so the
// parser keeps one pending version and adopts it when the Forge section
// starts.
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
		var specTitle string
		minecraftGameVersion := versionpkg.Unknown
		var gameVersionCandidates []versionpkg.Version
		s := bufio.NewScanner(r)
		for s.Scan() {
			line := s.Text()
			switch {
			case strings.HasPrefix(line, "Name: "):
				inForgeSection = false
				specTitle = ""
			case strings.HasPrefix(line, "Specification-Title: "):
				if after, found := strings.CutPrefix(
					line,
					"Specification-Title: ",
				); found {
					specTitle = strings.TrimSpace(after)
				}
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
					candidate := versionpkg.Version(after)
					gameVersionCandidates = append(
						gameVersionCandidates,
						candidate,
					)
					if specTitle == "Minecraft" {
						minecraftGameVersion = candidate
					}
				}
			}
		}
		switch {
		case minecraftGameVersion != versionpkg.Unknown:
			gameVersion = minecraftGameVersion
		default:
			// Without a Minecraft specification section, a manifest may
			// still repeat the game version. Skip the loader's own
			// specification, which shares the Forge version prefix.
			for index := len(gameVersionCandidates) - 1; index >= 0; index-- {
				candidate := gameVersionCandidates[index]
				if versionSharesPrefix(candidate, forgeVersion) {
					continue
				}
				gameVersion = candidate
				break
			}
		}

		break
	}

	return forgeVersion, gameVersion
}

// isMinecraftReleaseVersion reports whether value is a positively identified
// Minecraft release id. Minecraft uses dotted numeric ids: the classic 1.x
// line and the year-based 26.x line. Arbitrary plugin versions such as 2.5.0
// are not release ids.
func isMinecraftReleaseVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}
	return major == 1 || major >= 26
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

// versionSharesPrefix reports whether candidate is the loader version or one
// of its prefixes. Forge manifests repeat the loader specification version,
// which must not be mistaken for a Minecraft release id.
func versionSharesPrefix(candidate versionpkg.Version, loader versionpkg.Version) bool {
	if candidate == "" || loader == "" {
		return false
	}
	return candidate == loader ||
		strings.HasPrefix(string(loader), string(candidate)+".")
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

// parseLoaderArgs extracts the loader and Minecraft versions from the
// generated Forge/NeoForge argument sidecar. Both `--fml.forgeVersion` and
// `--fml.neoForgeVersion` are accepted because the installer uses the same
// argument shape for both products.
func parseLoaderArgs(data []byte) (
	loaderVersion versionpkg.Version,
	gameVersion versionpkg.Version,
) {
	loaderVersion = versionpkg.Unknown
	gameVersion = versionpkg.Unknown
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		for i := 0; i+1 < len(fields); i++ {
			switch fields[i] {
			case "--fml.forgeVersion", "--fml.neoForgeVersion":
				loaderVersion = versionpkg.Version(fields[i+1])
			case "--fml.mcVersion":
				gameVersion = versionpkg.Version(fields[i+1])
			}
		}
	}
	return loaderVersion, gameVersion
}

// parseLaunchTarget returns the value of --launchTarget in a generated loader
// argument sidecar. Server layouts name a server target; the client installer
// spec names a client target.
func parseLaunchTarget(data []byte) string {
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] == "--launchTarget" {
				return fields[i+1]
			}
		}
	}
	return ""
}
