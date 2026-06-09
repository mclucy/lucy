package detector

import (
	"archive/zip"
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/slugresolve"

	"github.com/pelletier/go-toml"
)

type forgeLegacyDetector struct{}

func (d *forgeLegacyDetector) Name() string {
	return "forge legacy server"
}

// Sources:
// - https://docs.minecraftforge.net/en/1.16.x/gettingstarted/
// - https://forums.minecraftforge.net/topic/102544-forge-370-minecraft-1171/
func (d *forgeLegacyDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (*ExecutableEvidence, error) {
	base := filepath.Base(filePath)
	if !strings.Contains(base, "forge-") || !strings.Contains(
		base,
		"universal",
	) {
		return nil, nil
	}

	forgeVersion, gameVersion := parseForgeManifest(zipReader)
	if !hasConcreteVersion(forgeVersion) {
		return nil, nil
	}
	if !hasConcreteVersion(gameVersion) {
		gameVersion, _, _ = parseForgeVersionTupleFromPath(filePath)
	}
	if !hasConcreteVersion(gameVersion) {
		return nil, nil
	}

	return buildForgeRuntimeInfo(filePath, gameVersion, forgeVersion), nil
}

type forgeModernDetector struct{}

func (d *forgeModernDetector) Name() string {
	return "forge modern server"
}

// Sources:
// - https://docs.minecraftforge.net/en/latest/gettingstarted/server/
// - https://forums.minecraftforge.net/topic/102544-forge-370-minecraft-1171/
func (d *forgeModernDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (*ExecutableEvidence, error) {
	gameVersion, forgeVersion, ok := parseForgeVersionTupleFromPath(filePath)
	if !ok || compareForgeMajor(forgeVersion, 61) >= 0 {
		return nil, nil
	}

	base := filepath.Base(filePath)
	if !strings.HasPrefix(base, "forge-") {
		return nil, nil
	}
	if !strings.Contains(base, "-server") && !strings.Contains(
		base,
		"-universal",
	) {
		return nil, nil
	}
	if !forgeHasSibling(filePath, "unix_args.txt", "win_args.txt") {
		return nil, nil
	}

	return buildForgeRuntimeInfo(filePath, gameVersion, forgeVersion), nil
}

type forgeLatestDetector struct{}

func (d *forgeLatestDetector) Name() string {
	return "forge latest server"
}

// Sources:
// - https://docs.minecraftforge.net/en/latest/gettingstarted/server/
// - https://forums.minecraftforge.net/topic/154652-how-to-install-forge-6110-for-1211-server/
func (d *forgeLatestDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (*ExecutableEvidence, error) {
	gameVersion, forgeVersion, ok := parseForgeVersionTupleFromPath(filePath)
	if !ok || compareForgeMajor(forgeVersion, 61) < 0 {
		return nil, nil
	}

	base := filepath.Base(filePath)
	if !strings.HasPrefix(base, "forge-") || !strings.Contains(
		base,
		"-server",
	) {
		return nil, nil
	}
	if !forgeHasSibling(
		filePath,
		"unix_args.txt",
		"win_args.txt",
		strings.Replace(base, "-server.jar", "-shim.jar", 1),
		strings.Replace(base, "-server.jar", "-universal.jar", 1),
	) {
		return nil, nil
	}

	return buildForgeRuntimeInfo(filePath, gameVersion, forgeVersion), nil
}

// forgeServerDetector detects Forge servers via manifest metadata fallback.
type forgeServerDetector struct{}

func (d *forgeServerDetector) Name() string {
	return "forge server"
}

// Sources:
// - https://docs.minecraftforge.net/en/latest/gettingstarted/server/
// - https://docs.minecraftforge.net/en/1.16.x/gettingstarted/
func (d *forgeServerDetector) Detect(
	filePath string,
	zipReader *zip.Reader,
	fileHandle *os.File,
) (*ExecutableEvidence, error) {
	forgeVersion, gameVersion := parseForgeManifest(zipReader)
	if !hasConcreteVersion(forgeVersion) {
		return nil, nil
	}
	if !hasConcreteVersion(gameVersion) {
		parsedGameVersion, _, ok := parseForgeVersionTupleFromPath(filePath)
		if ok {
			gameVersion = parsedGameVersion
		}
	}
	if !hasConcreteVersion(gameVersion) {
		return nil, nil
	}

	return buildForgeRuntimeInfo(filePath, gameVersion, forgeVersion), nil
}

func parseForgeManifest(
	zipReader *zip.Reader,
) (forgeVersion types.BareVersion, gameVersion types.BareVersion) {
	for _, f := range zipReader.File {
		if f.Name != "META-INF/MANIFEST.MF" {
			continue
		}

		r, err := f.Open()
		if err != nil {
			continue
		}
		defer tools.CloseReader(r, logger.Warn)

		s := bufio.NewScanner(r)
		for s.Scan() {
			line := s.Text()
			if line == "Implementation-Title: net.minecraftforge" {
				if !s.Scan() {
					continue
				}
				if after, found := strings.CutPrefix(
					s.Text(),
					"Implementation-Version: ",
				); found {
					forgeVersion = types.BareVersion(after)
				}
			}
			if strings.HasPrefix(line, "Specification-Version: ") {
				if after, found := strings.CutPrefix(
					line,
					"Specification-Version: ",
				); found && isMinecraftReleaseVersion(after) {
					gameVersion = types.BareVersion(after)
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

func compareForgeMajor(version types.BareVersion, target int) int {
	major := strings.Split(string(version), ".")[0]
	switch major {
	case "":
		return -1
	case "61":
		if target == 61 {
			return 0
		}
		if target < 61 {
			return 1
		}
		return -1
	default:
		if major > "61" {
			if target <= 61 {
				return 1
			}
			return -1
		}
		return -1
	}
}

// getForgeModVersion extracts the version from a Forge JAR's manifest
// when the mod version is set to `${file.jarVersion}`.
func getForgeModVersion(zipReader *zip.Reader) types.BareVersion {
	var r io.ReadCloser
	var err error
	for _, f := range zipReader.File {
		if f.Name == "META-INF/MANIFEST.MF" {
			r, err = f.Open()
			if err != nil {
				return types.VersionUnknown
			}
			defer tools.CloseReader(r, logger.Warn)
			break
		}
	}

	if r == nil {
		return types.VersionUnknown
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return types.VersionUnknown
	}
	manifest := string(data)
	const versionField = "Implementation-Version: "
	idx := strings.Index(manifest, versionField)
	if idx == -1 {
		return types.VersionUnknown
	}
	i := idx + len(versionField)
	v := manifest[i:]
	v = strings.Split(v, "\r")[0]
	v = strings.Split(v, "\n")[0]
	return types.BareVersion(v)
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

// forgeModDetector detects new Forge mods (1.13+)
type forgeModDetector struct{}

func (d *forgeModDetector) Name() string {
	return "forge mod"
}

func (d *forgeModDetector) Detect(
	zipReader *zip.Reader,
	fileHandle *os.File,
) (packages []types.Package, err error) {
	var wg sync.WaitGroup
	for _, f := range zipReader.File {
		if f.Name == "META-INF/mods.toml" {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer tools.CloseReader(r, logger.Warn)

			data, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}

			modIdentifier := &exttype.FileModLoaderIdentifier{}
			err = toml.Unmarshal(data, modIdentifier)
			if err != nil {
				return nil, err
			}

			for _, mod := range modIdentifier.Mods {
				if mod.ModID == "forge" {
					continue
				}

				version := types.BareVersion(mod.Version)
				if version == "${file.jarVersion}" {
					version = getForgeModVersion(zipReader)
				}

				rawDeps := modIdentifier.Dependencies[mod.ModID]
				depSpecs := make([]modLoaderDependencySpec, 0, len(rawDeps))
				for _, dep := range rawDeps {
					depSpecs = append(
						depSpecs, modLoaderDependencySpec{
							modID:        dep.ModID,
							mandatory:    dep.Mandatory,
							versionRange: dep.VersionRange,
						},
					)
				}

				p := translateModLoaderPackage(
					types.PlatformForge,
					fileHandle.Name(),
					mod.ModID,
					version,
					depSpecs,
					modIdentifier.License,
					mod.DisplayName,
					mod.Description,
					mod.Authors,
					mod.DisplayURL,
					modIdentifier.IssueTrackerURL,
				)

				packages = append(packages, p)

				var metaURLs []string
				for _, u := range p.Information.Urls {
					if u.Url != "" {
						metaURLs = append(metaURLs, u.Url)
					}
				}
				wg.Add(2)
				go func(name, path string, urls []string) {
					defer wg.Done()
					slugresolve.ResolveSlug(
						types.SourceModrinth,
						name,
						path,
						urls,
					)
				}(string(p.Id.Name), fileHandle.Name(), metaURLs)
				go func(name, path string, urls []string) {
					defer wg.Done()
					slugresolve.ResolveSlug(
						types.SourceCurseForge,
						name,
						path,
						urls,
					)
				}(string(p.Id.Name), fileHandle.Name(), metaURLs)
			}
		}
	}

	wg.Wait()
	return packages, nil
}

func init() {
	registerExecutableDetector(&forgeLegacyDetector{})
	registerExecutableDetector(&forgeModernDetector{})
	registerExecutableDetector(&forgeLatestDetector{})
	registerExecutableDetector(&forgeServerDetector{})
	registerModDetector(&forgeModDetector{})
}
