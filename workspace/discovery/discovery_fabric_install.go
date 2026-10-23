package discovery

import (
	"encoding/json/v2"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/catalog"
	versionpkg "github.com/mclucy/lucy/version"
)

// discoverFabric recognizes Fabric server distributions. Two layouts are
// legitimate:
//
//   - an installer shim (fabric-server-launch.jar) beside the vanilla jar
//     its fabric-server-launcher.properties names, with libraries referenced
//     from the shim manifest;
//   - an unattended meta launcher (fabric-server-mc.<mc>-loader.<loader>-
//     launcher.<installer>.jar) which, after its first run, keeps a payload
//     and generated launch jar under the hidden .fabric/server directory.
//
// The meta launcher is recognized before .fabric/server is materialized.
// Hidden files are claimed only when their content matches the identity the
// meta launcher embedded; stale files from another version stay unclaimed.
// A generated launcher without its meta launcher is still a real installed
// layout and is recognized from its own content.
func discoverFabric(s *scan) ([]Installation, []Finding) {
	if s == nil {
		return nil, nil
	}

	installations := make([]Installation, 0)
	metaLauncherSeen := false
	for _, file := range s.RootFiles() {
		if file.IsDirectory() || !isJarArtifact(file) {
			continue
		}
		if installation, ok := fabricMetaLauncherInstallation(s, file); ok {
			installations = append(installations, installation)
			metaLauncherSeen = true
			continue
		}
		if installation, ok := fabricShimInstallation(s, file); ok {
			installations = append(installations, installation)
		}
	}
	if !metaLauncherSeen {
		installations = append(
			installations,
			fabricGeneratedLauncherInstallations(s)...,
		)
	}
	return installations, nil
}

// fabricMetaLauncherInstallation recognizes the unattended launcher by its
// embedded install.properties and claims the hidden files the launcher
// generates on first run when their content agrees with that identity.
func fabricMetaLauncherInstallation(
	s *scan,
	file *artifact,
) (Installation, bool) {
	archive, err := file.Archive()
	if err != nil {
		return Installation{}, false
	}
	mainClass, ok := readArchiveManifestMainClass(archive)
	if !ok || mainClass != fabricMetaLauncherMainClass {
		return Installation{}, false
	}
	loaderVersion, gameVersion, ok := parseFabricInstallProperties(archive)
	if !ok {
		return Installation{}, false
	}

	installation := NewInstallation(
		fabricIdentity(loaderVersion, gameVersion),
		file.Path(),
	)

	dataDir := filepath.Join(s.root, filepath.FromSlash(fabricServerDataDir))
	generatedPath := filepath.Join(
		dataDir,
		fabricGeneratedLauncherPrefix+string(loaderVersion)+
			"-minecraft-"+string(gameVersion)+".jar",
	)
	if generated, ok := s.File(generatedPath); ok && !generated.IsDirectory() {
		generatedLoader, generatedGame, classPath, ok := fabricLaunchJarIdentity(generated)
		if ok && generatedLoader == loaderVersion && generatedGame == gameVersion {
			installation.AddFile(generated.Path(), RoleEntryPoint)
			addLocalClassPathFiles(s, &installation, generated, classPath)
			addFabricSidecars(s, &installation, generated)
		}
	}

	payloadPath := filepath.Join(dataDir, string(gameVersion)+"-server.jar")
	if payload, ok := s.File(payloadPath); ok && !payload.IsDirectory() &&
		fabricServerPayloadMatches(payload, gameVersion) {
		installation.AddFile(payload.Path(), RolePayload)
	}

	return installation, true
}

// fabricShimInstallation enriches a launch shim with the sidecars, payload
// and classpath files the shim needs at run time.
func fabricShimInstallation(s *scan, file *artifact) (Installation, bool) {
	archive, err := file.Archive()
	if err != nil {
		return Installation{}, false
	}
	properties, ok, err := readArchiveEntry(archive, fabricLaunchPropertiesPath)
	if err != nil || !ok || !fabricLauncherPropertiesAreServer(properties) {
		return Installation{}, false
	}

	loaderVersion, gameVersion, classPath := parseFabricLauncherManifest(archive)
	if loaderVersion == versionpkg.Unknown {
		loaderVersion = parseFabricLauncherBundledLoaderVersion(archive)
	}
	launcherProperties := []byte(nil)
	propertiesFile, hasProperties := s.Sibling(file, fabricLauncherPropertiesPath)
	if hasProperties && !propertiesFile.IsDirectory() {
		launcherProperties, _ = propertiesFile.Read()
	}
	if gameVersion == versionpkg.Unknown {
		gameVersion = parseFabricLauncherSidecarGameVersion(s, file)
	}
	// Installer 1.1+ shims embed no version evidence, so the last resort is
	// the vanilla jar the shim boots: FabricServerLauncher reads serverJar
	// from fabric-server-launcher.properties and defaults to server.jar.
	serverJarPath := fabricLauncherServerJarPath(file.Path(), launcherProperties)
	serverJar, hasServerJar := s.File(serverJarPath)
	if hasServerJar && serverJar.IsDirectory() {
		hasServerJar = false
	}
	if gameVersion == versionpkg.Unknown && hasServerJar {
		gameVersion = parseServerJarGameVersion(serverJar)
	}
	if loaderVersion == versionpkg.Unknown || gameVersion == versionpkg.Unknown {
		return Installation{}, false
	}

	installation := NewInstallation(
		fabricIdentity(loaderVersion, gameVersion),
		file.Path(),
	)
	if hasServerJar {
		installation.AddFile(serverJar.Path(), RolePayload)
	}
	if hasProperties {
		installation.AddFile(propertiesFile.Path(), RoleConfiguration)
	}
	if sidecar, ok := s.Sibling(file, fabricVersionSidecarPath); ok &&
		!sidecar.IsDirectory() {
		installation.AddFile(sidecar.Path(), RoleConfiguration)
	}
	addLocalClassPathFiles(s, &installation, file, classPath)
	return installation, true
}

// fabricGeneratedLauncherInstallations recognizes a generated launch jar in
// the hidden server data directory when no meta launcher remains.
func fabricGeneratedLauncherInstallations(s *scan) []Installation {
	dataDir := filepath.Join(s.root, filepath.FromSlash(fabricServerDataDir))
	entries, err := s.ReadDir(dataDir)
	if err != nil {
		return nil
	}

	installations := make([]Installation, 0)
	for _, entry := range entries {
		if entry.IsDir() ||
			!strings.HasPrefix(entry.Name(), fabricGeneratedLauncherPrefix) ||
			!strings.HasSuffix(strings.ToLower(entry.Name()), ".jar") {
			continue
		}
		file, ok := s.File(filepath.Join(dataDir, entry.Name()))
		if !ok || file.IsDirectory() {
			continue
		}
		loaderVersion, gameVersion, classPath, ok := fabricLaunchJarIdentity(file)
		if !ok {
			continue
		}
		installation := NewInstallation(
			fabricIdentity(loaderVersion, gameVersion),
			file.Path(),
		)
		addLocalClassPathFiles(s, &installation, file, classPath)
		addFabricSidecars(s, &installation, file)

		payloadPath := filepath.Join(dataDir, string(gameVersion)+"-server.jar")
		if payload, ok := s.File(payloadPath); ok && !payload.IsDirectory() &&
			fabricServerPayloadMatches(payload, gameVersion) {
			installation.AddFile(payload.Path(), RolePayload)
		}
		installations = append(installations, installation)
	}
	return installations
}

// addFabricSidecars claims the launcher properties and version metadata that
// sit beside a generated launch jar.
func addFabricSidecars(s *scan, installation *Installation, launcher *artifact) {
	if properties, ok := s.Sibling(launcher, fabricLauncherPropertiesPath); ok &&
		!properties.IsDirectory() {
		installation.AddFile(properties.Path(), RoleConfiguration)
	}
	if sidecar, ok := s.Sibling(launcher, fabricVersionSidecarPath); ok &&
		!sidecar.IsDirectory() {
		installation.AddFile(sidecar.Path(), RoleConfiguration)
	}
}

// fabricServerPayloadMatches verifies that a downloaded server jar carries
// the game version the meta launcher embedded. The id field is authoritative
// for modern servers; name and release_target cover older ids that append a
// build hash.
func fabricServerPayloadMatches(
	file *artifact,
	gameVersion versionpkg.Version,
) bool {
	archive, err := file.Archive()
	if err != nil {
		return false
	}
	data, ok, err := readArchiveEntry(archive, mojangVersionJSONEntry)
	if err != nil || !ok {
		return false
	}
	version, guarded, err := mojangVersionFromJSON(data)
	if err != nil || guarded {
		return false
	}
	if version == gameVersion {
		return true
	}

	var metadata struct {
		Name          string `json:"name"`
		ReleaseTarget string `json:"release_target"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return false
	}
	return versionpkg.Version(strings.TrimSpace(metadata.Name)) == gameVersion ||
		versionpkg.Version(strings.TrimSpace(metadata.ReleaseTarget)) == gameVersion
}

// fabricLaunchJarIdentity validates a generated Fabric launch jar from its
// content. The filename alone is never sufficient.
func fabricLaunchJarIdentity(file *artifact) (
	loaderVersion versionpkg.Version,
	gameVersion versionpkg.Version,
	classPath []string,
	ok bool,
) {
	archive, err := file.Archive()
	if err != nil {
		return versionpkg.Unknown, versionpkg.Unknown, nil, false
	}
	loaderVersion, gameVersion, classPath = parseFabricLauncherManifest(archive)
	if loaderVersion == versionpkg.Unknown || gameVersion == versionpkg.Unknown {
		return versionpkg.Unknown, versionpkg.Unknown, nil, false
	}
	properties, found, err := readArchiveEntry(archive, fabricLaunchPropertiesPath)
	if err != nil || !found || !fabricLauncherPropertiesAreServer(properties) {
		return versionpkg.Unknown, versionpkg.Unknown, nil, false
	}
	return loaderVersion, gameVersion, classPath, true
}

// addMatchingMinecraftPayload claims the root Minecraft server jar as the
// installation payload when its content names the same game version. The
// engine then treats the matching vanilla installation as payload rather
// than a second server.
func addMatchingMinecraftPayload(
	s *scan,
	installation *Installation,
	gameVersion versionpkg.Version,
) {
	if s == nil || installation == nil || !hasConcreteVersion(gameVersion) {
		return
	}
	serverJar, ok := s.File(filepath.Join(s.root, minecraftServerJarName))
	if !ok || serverJar.IsDirectory() {
		return
	}
	if parseServerJarGameVersion(serverJar) != gameVersion {
		return
	}
	installation.AddFile(serverJar.Path(), RolePayload)
}

// addMinecraftServerLibraryPayload claims the Minecraft server jar that
// installers keep under libraries/net/minecraft/server/<mc>/. The path names
// the game version; a version.json inside the jar must agree when present.
func addMinecraftServerLibraryPayload(
	s *scan,
	installation *Installation,
	gameVersion versionpkg.Version,
) {
	if s == nil || installation == nil || !hasConcreteVersion(gameVersion) {
		return
	}
	version := string(gameVersion)
	for _, name := range []string{
		"server-" + version + ".jar",
		"server-" + version + "-bundled.jar",
	} {
		path := filepath.Join(
			s.root,
			"libraries",
			"net",
			"minecraft",
			"server",
			version,
			name,
		)
		file, ok := s.File(path)
		if !ok || file.IsDirectory() {
			continue
		}
		if recorded := parseServerJarGameVersion(file); recorded != versionpkg.Unknown &&
			recorded != gameVersion {
			continue
		}
		installation.AddFile(file.Path(), RolePayload)
	}
}

// addLocalClassPathFiles resolves manifest Class-Path entries against the jar
// that declares them and claims the local files that exist. Class-Path
// entries must stay inside the scanned directory.
func addLocalClassPathFiles(
	s *scan,
	installation *Installation,
	jar *artifact,
	classPath []string,
) {
	for _, entry := range classPath {
		entry = strings.Trim(strings.TrimSpace(entry), `"`)
		if entry == "" || strings.Contains(entry, "://") ||
			strings.HasPrefix(entry, "file:") {
			continue
		}
		path := filepath.FromSlash(entry)
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(jar.Path()), path)
		}
		if file, ok := s.File(path); ok && !file.IsDirectory() {
			installation.AddFile(file.Path(), RoleLibrary)
		}
	}
}

func fabricIdentity(
	loaderVersion versionpkg.Version,
	gameVersion versionpkg.Version,
) Identity {
	return Identity{
		Primary: catalog.VersionedRef{
			Ecosystem: catalog.EcosystemFabric,
			Name:      "fabric",
			Version:   loaderVersion,
		},
		Components: []catalog.VersionedRef{
			{
				Ecosystem: catalog.EcosystemFabric,
				Name:      "fabric-loader",
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

// isJarArtifact reports whether a scanned file is a jar by name. Content
// inspection decides whether it is a real installation; the name only avoids
// opening unrelated files.
func isJarArtifact(file *artifact) bool {
	return strings.HasSuffix(strings.ToLower(file.Path()), ".jar")
}
