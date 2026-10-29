package discovery

import (
	"archive/zip"
	"bufio"
	"encoding/json/v2"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/internal/fileschema"
	versionpkg "github.com/mclucy/lucy/version"
)

const (
	fabricInstallPropertiesPath   = "install.properties"
	fabricLaunchPropertiesPath    = "fabric-server-launch.properties"
	fabricMetaLauncherMainClass   = "net.fabricmc.installer.ServerLauncher"
	fabricServerDataDir           = ".fabric/server"
	fabricGeneratedLauncherPrefix = "fabric-loader-server-"
	fabricVersionSidecarPath      = "version.json"
	// fabricLauncherPropertiesPath is the sidecar file FabricServerLauncher
	// keeps beside the shim; note the extra "r" versus
	// fabricLaunchPropertiesPath, which sits inside the jar.
	fabricLauncherPropertiesPath = "fabric-server-launcher.properties"
	fabricServerJarProperty      = "serverJar"
	minecraftServerJarName       = "server.jar"
	fabricModernKnotServer       = "net.fabricmc.loader.impl.launch.knot.KnotServer"
	fabricLegacyKnotServer       = "net.fabricmc.loader.launch.knot.KnotServer"
)

// parseFabricInstallProperties reads the unattended install.properties entry
// that a Fabric meta launcher embeds. Only a complete loader and game version
// pair counts as a runtime identity.
func parseFabricInstallProperties(zipReader *zip.Reader) (
	loaderVersion versionpkg.Version,
	gameVersion versionpkg.Version,
	ok bool,
) {
	loaderVersion = versionpkg.Unknown
	gameVersion = versionpkg.Unknown
	for _, f := range zipReader.File {
		if f.Name != fabricInstallPropertiesPath {
			continue
		}
		r, err := f.Open()
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if after, found := strings.CutPrefix(
				line,
				"fabric-loader-version=",
			); found {
				loaderVersion = versionpkg.Version(strings.TrimSpace(after))
			} else if after, found := strings.CutPrefix(
				line,
				"game-version=",
			); found {
				gameVersion = versionpkg.Version(strings.TrimSpace(after))
			}
		}
		scanErr := scanner.Err()
		_ = r.Close()
		if scanErr == nil && loaderVersion != versionpkg.Unknown &&
			gameVersion != versionpkg.Unknown {
			return loaderVersion, gameVersion, true
		}
	}

	return loaderVersion, gameVersion, false
}

func fabricLauncherPropertiesAreServer(data []byte) bool {
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if after, found := strings.CutPrefix(line, "launch.mainClass="); found {
			return after == fabricModernKnotServer || after == fabricLegacyKnotServer
		}
	}

	return false
}

// parseFabricLauncherManifest returns the loader and game versions named by
// the Class-Path entries of a Fabric launch jar manifest, together with the
// raw classpath. Installer shims keep library paths relative to the root;
// generated .fabric/server launchers walk back with "./../..".
func parseFabricLauncherManifest(zipReader *zip.Reader) (
	loaderVersion versionpkg.Version,
	gameVersion versionpkg.Version,
	classPath []string,
) {
	loaderVersion = versionpkg.Unknown
	gameVersion = versionpkg.Unknown
	classPath = parseArchiveClassPath(zipReader)
	for _, entry := range classPath {
		if parsed, found := fabricClassPathVersion(entry, "fabric-loader"); found {
			loaderVersion = parsed
		} else if parsed, found := fabricClassPathVersion(entry, "intermediary"); found {
			gameVersion = parsed
		}
	}
	return loaderVersion, gameVersion, classPath
}

// fabricClassPathVersion extracts the version segment after
// libraries/net/fabricmc/<artifact>/ in a manifest Class-Path entry.
func fabricClassPathVersion(classPath, artifact string) (versionpkg.Version, bool) {
	segments := strings.Split(filepath.ToSlash(classPath), "/")
	for i := 0; i+4 < len(segments); i++ {
		if segments[i] != "libraries" || segments[i+1] != "net" ||
			segments[i+2] != "fabricmc" || segments[i+3] != artifact {
			continue
		}
		candidate := segments[i+4]
		if candidate == "" || candidate == "." || candidate == ".." {
			return versionpkg.Unknown, false
		}
		return versionpkg.Version(candidate), true
	}
	return versionpkg.Unknown, false
}

// readArchiveManifestMainClass returns the Main-Class attribute of a jar
// manifest, unfolding its continuation lines first.
func readArchiveManifestMainClass(zipReader *zip.Reader) (string, bool) {
	data, ok, err := readArchiveEntry(zipReader, "META-INF/MANIFEST.MF")
	if err != nil || !ok {
		return "", false
	}
	for _, line := range unfoldManifestLines(data) {
		if value, found := strings.CutPrefix(line, "Main-Class: "); found {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

// parseArchiveClassPath returns the entries of the first Class-Path
// attribute in a jar manifest.
func parseArchiveClassPath(zipReader *zip.Reader) []string {
	data, ok, err := readArchiveEntry(zipReader, "META-INF/MANIFEST.MF")
	if err != nil || !ok {
		return nil
	}
	for _, line := range unfoldManifestLines(data) {
		value, found := strings.CutPrefix(line, "Class-Path: ")
		if !found {
			continue
		}
		return strings.Fields(value)
	}
	return nil
}

// unfoldManifestLines joins the continuation lines of a JAR manifest. A
// continuation line starts with a single space that is not part of the value.
func unfoldManifestLines(data []byte) []string {
	raw := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, " ") && len(lines) > 0 {
			lines[len(lines)-1] += line[1:]
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func parseFabricLauncherBundledLoaderVersion(zipReader *zip.Reader) versionpkg.Version {
	data, ok, err := readArchiveEntry(zipReader, "fabric.mod.json")
	if err != nil || !ok {
		return versionpkg.Unknown
	}

	var metadata fileschema.FileFabricModIdentifier
	if err := json.Unmarshal(data, &metadata); err != nil {
		return versionpkg.Unknown
	}
	if metadata.ID != "fabricloader" || strings.TrimSpace(metadata.Version) == "" {
		return versionpkg.Unknown
	}

	return versionpkg.Version(metadata.Version)
}

func parseFabricLauncherSidecarGameVersion(
	s *scan,
	primaryFile *artifact,
) versionpkg.Version {
	sidecar, ok := s.Sibling(primaryFile, fabricVersionSidecarPath)
	if !ok || sidecar.IsDirectory() {
		return versionpkg.Unknown
	}
	data, err := sidecar.Read()
	if err != nil {
		return versionpkg.Unknown
	}

	var metadata struct {
		Name          string `json:"name"`
		ReleaseTarget string `json:"release_target"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return versionpkg.Unknown
	}
	if strings.TrimSpace(metadata.Name) != "" {
		return versionpkg.Version(metadata.Name)
	}
	if strings.TrimSpace(metadata.ReleaseTarget) != "" {
		return versionpkg.Version(metadata.ReleaseTarget)
	}

	return versionpkg.Unknown
}

// parseServerJarGameVersion reads the game version from the version.json of a
// server jar on disk. Legacy ids embed a build hash after " / "; the name and
// release_target fields carry the clean release id. Any problem yields
// VersionUnknown: this source is the last resort for launch shims with no
// embedded version evidence.
func parseServerJarGameVersion(file *artifact) versionpkg.Version {
	zipReader, err := file.Archive()
	if err != nil {
		return versionpkg.Unknown
	}
	data, ok, err := readArchiveEntry(zipReader, mojangVersionJSONEntry)
	if err != nil || !ok {
		return versionpkg.Unknown
	}
	version, guarded, err := mojangVersionFromJSON(data)
	if err != nil || guarded {
		return versionpkg.Unknown
	}
	if isMinecraftReleaseVersion(string(version)) {
		return version
	}

	var metadata struct {
		Name          string `json:"name"`
		ReleaseTarget string `json:"release_target"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return versionpkg.Unknown
	}
	for _, candidate := range []string{
		strings.TrimSpace(metadata.Name),
		strings.TrimSpace(metadata.ReleaseTarget),
	} {
		if isMinecraftReleaseVersion(candidate) {
			return versionpkg.Version(candidate)
		}
	}
	return versionpkg.Unknown
}

// fabricLauncherServerJarPath returns the server jar that the launch shim at
// primaryPath boots. FabricServerLauncher reads serverJar from
// fabric-server-launcher.properties beside the shim and falls back to
// server.jar when the file or the entry is missing.
func fabricLauncherServerJarPath(
	primaryPath string,
	properties []byte,
) string {
	name := minecraftServerJarName
	if value := propertiesValue(properties, fabricServerJarProperty); value != "" {
		name = value
	}
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	return filepath.Join(filepath.Dir(primaryPath), name)
}

// propertiesValue returns the value of key in a Java properties payload, or
// "" when the key is absent.
func propertiesValue(data []byte, key string) string {
	var value string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}

		separator := strings.IndexAny(line, "=:\t ")
		if separator < 0 {
			continue
		}
		propertyKey := strings.TrimSpace(line[:separator])
		if propertyKey != key {
			continue
		}
		propertyValue := strings.TrimLeft(line[separator:], "=:\t ")
		value = strings.TrimSpace(propertyValue)
	}
	return value
}
