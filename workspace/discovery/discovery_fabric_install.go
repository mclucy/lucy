package discovery

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"

	versionpkg "github.com/mclucy/lucy/version"
)

// FabricInstallationRuntimes detects Fabric server distributions that span
// more than one file. Two layouts are legitimate:
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
// Each legitimate layout yields one evidence. Identity grouping belongs to
// the workspace scan.
func FabricInstallationRuntimes(context DetectionContext) []*RuntimeEvidence {
	if context.root == "" {
		return nil
	}

	runtimes := make([]*RuntimeEvidence, 0)
	for _, file := range context.RootFiles() {
		if file.IsDirectory() || !strings.HasSuffix(strings.ToLower(file.Path()), ".jar") {
			continue
		}
		if runtime := detectFabricMetaLauncherInstallation(context, file); runtime != nil {
			runtimes = append(runtimes, runtime)
			continue
		}
		if runtime := detectFabricShimInstallation(context, file); runtime != nil {
			runtimes = append(runtimes, runtime)
		}
	}
	return runtimes
}

// detectFabricMetaLauncherInstallation recognizes the unattended launcher by
// its embedded install.properties. The installer jar users download carries a
// different Main-Class and no install.properties, so it never becomes a
// runnable server here.
func detectFabricMetaLauncherInstallation(
	context DetectionContext,
	file *DetectionFile,
) *RuntimeEvidence {
	archive, err := file.Archive()
	if err != nil || archive == nil {
		return nil
	}
	mainClass, ok := readArchiveManifestMainClass(archive)
	if !ok || mainClass != fabricMetaLauncherMainClass {
		return nil
	}

	loaderVersion, gameVersion, ok := parseFabricInstallProperties(archive)
	if !ok {
		return nil
	}

	evidence := newFabricRuntimeEvidence(file.Path(), loaderVersion, gameVersion)
	evidence.DetectorName = "fabric installation"
	evidence.EntryPoints = []string{file.Path()}
	installationFiles := []string{file.Path()}

	dataDir := filepath.Join(context.root, filepath.FromSlash(fabricServerDataDir))

	// ServerLauncher names the generated fabric-loader-server-<loader>-
	// minecraft-<game>.jar after both versions. Only a jar whose manifest
	// agrees with the embedded identity belongs to this installation.
	generatedPath := filepath.Join(
		dataDir,
		fabricGeneratedLauncherPrefix+string(loaderVersion)+
			"-minecraft-"+string(gameVersion)+".jar",
	)
	if generated, ok := detectionFileInRoot(context.root, generatedPath); ok {
		generatedLoader, generatedGame, classPath, ok := fabricLaunchJarIdentity(generated)
		if ok && generatedLoader == loaderVersion && generatedGame == gameVersion {
			evidence.EntryPoints = append(evidence.EntryPoints, generated.Path())
			installationFiles = append(installationFiles, generated.Path())
			installationFiles = append(
				installationFiles,
				fabricClassPathFiles(context.root, generated, classPath)...,
			)
		}
	}

	// ServerLauncher downloads <game>-server.jar beside the generated launch
	// jar. The payload version.json decides whether the file belongs to this
	// identity; a different game version is a stale leftover, not payload.
	payloadPath := filepath.Join(dataDir, string(gameVersion)+"-server.jar")
	if payload, ok := detectionFileInRoot(context.root, payloadPath); ok &&
		fabricServerPayloadMatches(payload, gameVersion) {
		evidence.ConsumedFiles = []*DetectionFile{payload}
		installationFiles = append(installationFiles, payload.Path())
	}

	evidence.InstallationFiles = uniquePaths(installationFiles)
	return evidence
}

// detectFabricShimInstallation enriches the per-file shim evidence with the
// sidecars, payload and classpath files the shim needs at run time.
func detectFabricShimInstallation(
	context DetectionContext,
	file *DetectionFile,
) *RuntimeEvidence {
	evidence, err := (&fabricServerLauncherDetector{}).Detect(context, file)
	if err != nil || evidence == nil || evidence.PrimaryRuntime == nil {
		return nil
	}

	evidence.DetectorName = "fabric installation"
	evidence.EntryPoints = []string{file.Path()}
	installationFiles := []string{file.Path()}
	consumedPayloads := make(map[string]bool, len(evidence.ConsumedFiles))
	for _, consumed := range evidence.ConsumedFiles {
		if consumed != nil && !consumed.IsDirectory() {
			consumedPayloads[consumed.Path()] = true
			installationFiles = append(installationFiles, consumed.Path())
		}
	}
	launcherProperties := []byte(nil)
	if properties, ok := context.Sibling(file, fabricLauncherPropertiesPath); ok &&
		!properties.IsDirectory() {
		installationFiles = append(installationFiles, properties.Path())
		launcherProperties, _ = properties.Read()
	}
	// FabricServerLauncher resolves serverJar relative to the shim. The
	// per-file detector only sees root files, so resolve nested payloads here.
	serverJarPath := fabricLauncherServerJarPath(file, launcherProperties)
	if payload, ok := detectionFileInRoot(context.root, serverJarPath); ok &&
		!consumedPayloads[payload.Path()] {
		evidence.ConsumedFiles = append(evidence.ConsumedFiles, payload)
		installationFiles = append(installationFiles, payload.Path())
	}
	if sidecar, ok := context.Sibling(file, fabricVersionSidecarPath); ok &&
		!sidecar.IsDirectory() {
		installationFiles = append(installationFiles, sidecar.Path())
	}

	archive, err := file.Archive()
	if err == nil && archive != nil {
		_, _, classPath := parseFabricLauncherManifest(archive)
		installationFiles = append(
			installationFiles,
			fabricClassPathFiles(context.root, file, classPath)...,
		)
	}

	evidence.InstallationFiles = uniquePaths(installationFiles)
	return evidence
}

// fabricServerPayloadMatches verifies that a downloaded server jar carries
// the game version the meta launcher embedded. The id field is authoritative
// for modern servers; name and release_target cover older ids that append a
// build hash.
func fabricServerPayloadMatches(file *DetectionFile, gameVersion versionpkg.Version) bool {
	archive, err := file.Archive()
	if err != nil || archive == nil {
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
func fabricLaunchJarIdentity(file *DetectionFile) (
	loaderVersion versionpkg.Version,
	gameVersion versionpkg.Version,
	classPath []string,
	ok bool,
) {
	archive, err := file.Archive()
	if err != nil || archive == nil {
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

// detectionFileInRoot returns a regular file only when path stays inside the
// scanned directory. Class-Path entries and hidden layout files must not
// escape the workspace.
func detectionFileInRoot(root, path string) (*DetectionFile, bool) {
	if root == "" {
		return nil, false
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, false
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil || !filepath.IsLocal(relative) {
		return nil, false
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		return nil, false
	}
	return NewDetectionFile(absolute), true
}

// fabricClassPathFiles resolves manifest Class-Path entries against the jar
// that declares them and keeps the local files that exist.
func fabricClassPathFiles(
	root string,
	jar *DetectionFile,
	classPath []string,
) []string {
	files := make([]string, 0, len(classPath))
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
		if file, ok := detectionFileInRoot(root, path); ok {
			files = append(files, file.Path())
		}
	}
	return files
}

func uniquePaths(paths []string) []string {
	unique := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		unique = append(unique, path)
	}
	return unique
}
