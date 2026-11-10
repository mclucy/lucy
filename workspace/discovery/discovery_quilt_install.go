package discovery

import (
	"archive/zip"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/catalog"
	versionpkg "github.com/mclucy/lucy/version"
)

// Quilt's server layout mirrors Fabric's launch shim: a manifest-only
// quilt-server-launch.jar boots the vanilla server jar named by
// quilt-server-launcher.properties. The launcher's Main-Class and manifest
// Class-Path name the loader and libraries; the first run extracts the vanilla
// bundler into versions/<game>/server-<game>.jar and writes a transform cache
// under .cache/quilt_loader.
const (
	quiltLauncherMainClass        = "org.quiltmc.loader.impl.launch.server.QuiltServerLauncher"
	quiltKnotServerMainClass      = "org.quiltmc.loader.impl.launch.knot.KnotServer"
	quiltLaunchPropertiesPath     = "quilt-server-launch.properties"
	quiltLauncherPropertiesPath   = "quilt-server-launcher.properties"
	quiltServerJarProperty        = "serverJar"
	quiltFirstRunVersionsDir      = "versions"
	quiltLoaderLibraryGroup       = "org/quiltmc/quilt-loader"
	quiltHashedLibraryGroup       = "org/quiltmc/hashed"
	quiltIntermediaryLibraryGroup = "net/fabricmc/intermediary"
)

// discoverQuilt recognizes an installed Quilt server from the launcher JAR.
// Identity comes from the manifest Class-Path, never from the filename: a
// Fabric launcher declares a different Main-Class and is rejected here.
func discoverQuilt(s *scan) ([]Installation, []Finding) {
	if s == nil {
		return nil, nil
	}

	installations := make([]Installation, 0)
	for _, file := range s.RootFiles() {
		if file.IsDirectory() || !isJarArtifact(file) {
			continue
		}
		if installation, ok := quiltLauncherInstallation(s, file); ok {
			installations = append(installations, installation)
		}
	}
	return installations, nil
}

// quiltLauncherInstallation proves the launcher identity and claims the
// sidecars, payload, first-run extraction, and classpath files it uses.
func quiltLauncherInstallation(s *scan, file *artifact) (Installation, bool) {
	archive, err := file.Archive()
	if err != nil {
		return Installation{}, false
	}
	mainClass, ok := readArchiveManifestMainClass(archive)
	if !ok || mainClass != quiltLauncherMainClass {
		return Installation{}, false
	}
	if !quiltLaunchPropertiesMatch(archive) {
		return Installation{}, false
	}

	loaderVersion, gameVersion, classPath := parseQuiltLauncherManifest(archive)
	if loaderVersion == versionpkg.Unknown || gameVersion == versionpkg.Unknown {
		return Installation{}, false
	}

	installation := NewInstallation(
		quiltIdentity(loaderVersion, gameVersion),
		file.Path(),
	)
	addLocalClassPathFiles(s, &installation, file, classPath)
	addQuiltLauncherSidecar(s, &installation, file)
	addQuiltServerPayload(s, &installation, file, gameVersion)
	addQuiltFirstRunPayloads(s, &installation, gameVersion)
	return installation, true
}

// quiltLaunchPropertiesMatch validates the launch properties a generated
// launcher embeds. The official installer embeds none, so only a present entry
// must name Quilt's KnotServer main class.
func quiltLaunchPropertiesMatch(archive *zip.Reader) bool {
	data, found, err := readArchiveEntry(archive, quiltLaunchPropertiesPath)
	if err != nil {
		return false
	}
	if !found {
		return true
	}
	return propertiesValue(data, "launch.mainClass") == quiltKnotServerMainClass
}

// parseQuiltLauncherManifest returns the loader and game versions named by the
// Class-Path entries of a Quilt launcher. The hashed artifact carries the game
// version; intermediary is the fallback for layouts without it.
func parseQuiltLauncherManifest(zipReader *zip.Reader) (
	loaderVersion versionpkg.Version,
	gameVersion versionpkg.Version,
	classPath []string,
) {
	loaderVersion = versionpkg.Unknown
	classPath = parseArchiveClassPath(zipReader)

	hashedGameVersion := versionpkg.Unknown
	intermediaryGameVersion := versionpkg.Unknown
	for _, entry := range classPath {
		if parsed, found := quiltClassPathVersion(entry, quiltLoaderLibraryGroup); found {
			loaderVersion = parsed
		}
		if parsed, found := quiltClassPathVersion(entry, quiltHashedLibraryGroup); found {
			hashedGameVersion = parsed
		}
		if parsed, found := quiltClassPathVersion(entry, quiltIntermediaryLibraryGroup); found {
			intermediaryGameVersion = parsed
		}
	}
	if hashedGameVersion != versionpkg.Unknown {
		gameVersion = hashedGameVersion
	} else {
		gameVersion = intermediaryGameVersion
	}
	return loaderVersion, gameVersion, classPath
}

// quiltClassPathVersion extracts the version segment after groupPath in a
// manifest Class-Path entry, for example
// libraries/org/quiltmc/quilt-loader/0.27.1/quilt-loader-0.27.1.jar.
func quiltClassPathVersion(
	classPath string,
	groupPath string,
) (versionpkg.Version, bool) {
	segments := strings.Split(filepath.ToSlash(classPath), "/")
	group := strings.Split(groupPath, "/")
	for i := 0; i+len(group)+1 < len(segments); i++ {
		if segments[i] != "libraries" {
			continue
		}
		matched := true
		for offset, part := range group {
			if segments[i+1+offset] != part {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		candidate := segments[i+len(group)+1]
		if candidate == "" || candidate == "." || candidate == ".." {
			continue
		}
		return versionpkg.Version(candidate), true
	}
	return versionpkg.Unknown, false
}

// addQuiltLauncherSidecar claims the properties file the launcher reads for
// the server jar location. It is configuration, not payload evidence.
func addQuiltLauncherSidecar(
	s *scan,
	installation *Installation,
	launcher *artifact,
) {
	properties, ok := s.Sibling(launcher, quiltLauncherPropertiesPath)
	if !ok || properties.IsDirectory() {
		return
	}
	installation.AddFile(properties.Path(), RoleConfiguration)
}

// addQuiltServerPayload claims the jar the launcher boots when its content
// names the observed game version. The sidecar may name another location;
// the launcher defaults to server.jar in its own directory.
func addQuiltServerPayload(
	s *scan,
	installation *Installation,
	launcher *artifact,
	gameVersion versionpkg.Version,
) {
	serverJarPath := filepath.Join(filepath.Dir(launcher.Path()), minecraftServerJarName)
	if properties, ok := s.Sibling(launcher, quiltLauncherPropertiesPath); ok &&
		!properties.IsDirectory() {
		if data, err := properties.Read(); err == nil {
			serverJarPath = quiltLauncherServerJarPath(launcher.Path(), data)
		}
	}

	payload, ok := s.File(serverJarPath)
	if !ok || payload.IsDirectory() ||
		!quiltServerPayloadMatches(payload, gameVersion) {
		return
	}
	installation.AddFile(payload.Path(), RolePayload)
}

// addQuiltFirstRunPayloads claims the vanilla jar the bundler extracts on first
// run. The path names the game version, and version.json must agree; cache
// entries carry no identity evidence and stay unclaimed.
func addQuiltFirstRunPayloads(
	s *scan,
	installation *Installation,
	gameVersion versionpkg.Version,
) {
	if s == nil || installation == nil || !hasConcreteVersion(gameVersion) {
		return
	}

	version := string(gameVersion)
	dir := filepath.Join(s.root, quiltFirstRunVersionsDir, version)
	for _, name := range []string{
		"server-" + version + ".jar",
		version + ".jar",
	} {
		payload, ok := s.File(filepath.Join(dir, name))
		if !ok || payload.IsDirectory() ||
			!quiltServerPayloadMatches(payload, gameVersion) {
			continue
		}
		installation.AddFile(payload.Path(), RolePayload)
	}
}

// quiltServerPayloadMatches reports whether a server jar carries the observed
// game version. A missing or guarded version.json proves nothing.
func quiltServerPayloadMatches(
	file *artifact,
	gameVersion versionpkg.Version,
) bool {
	if !hasConcreteVersion(gameVersion) {
		return false
	}
	return parseServerJarGameVersion(file) == gameVersion
}

// quiltLauncherServerJarPath returns the server jar that the Quilt launch
// launcher boots. QuiltServerLauncher reads serverJar from
// quilt-server-launcher.properties beside the launcher and falls back to
// server.jar when the file or the entry is missing.
func quiltLauncherServerJarPath(
	launcherPath string,
	properties []byte,
) string {
	name := minecraftServerJarName
	if value := propertiesValue(properties, quiltServerJarProperty); value != "" {
		name = value
	}
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	return filepath.Join(filepath.Dir(launcherPath), name)
}

func quiltIdentity(
	loaderVersion versionpkg.Version,
	gameVersion versionpkg.Version,
) Identity {
	return Identity{
		Primary: catalog.VersionedRef{
			Ecosystem: catalog.EcosystemQuilt,
			Name:      "quilt",
			Version:   loaderVersion,
		},
		Components: []catalog.VersionedRef{
			{
				Ecosystem: catalog.EcosystemQuilt,
				Name:      "quilt-loader",
				Version:   loaderVersion,
			},
			{
				Ecosystem: catalog.EcosystemMinecraft,
				Name:      "minecraft",
				Version:   gameVersion,
			},
		},
	}
}
