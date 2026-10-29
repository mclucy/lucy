package discovery

import (
	"archive/zip"
	"encoding/json/v2"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/catalog"
	versionpkg "github.com/mclucy/lucy/version"
)

const (
	neoForgeLibraryRoot           = "libraries/net/neoforged/neoforge"
	neoForgePatchedServerRoot     = "libraries/net/neoforged/minecraft-server-patched"
	neoForgeVersionPropertiesPath = "net/neoforged/neoforge/common/version.properties"
	neoForgeModsTomlPath          = "META-INF/neoforge.mods.toml"
)

// discoverNeoForge recognizes installed NeoForge servers under the NeoForge
// Maven library directory. The installer only provisions the layout; the
// runtime identity and its members are proven from local archives and the
// generated argument sidecars. The universal jar is the entry point when no
// server jar is present; otherwise it is a runtime library.
func discoverNeoForge(s *scan) ([]Installation, []Finding) {
	if s == nil {
		return nil, nil
	}

	root := filepath.Join(s.root, filepath.FromSlash(neoForgeLibraryRoot))
	entries, err := s.ReadDir(root)
	if err != nil {
		return nil, nil
	}

	installations := make([]Installation, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		installation, ok := neoForgeInstallation(
			s,
			filepath.Join(root, entry.Name()),
		)
		if ok {
			installations = append(installations, installation)
		}
	}
	return installations, nil
}

// neoForgeInstallation builds one installation from a
// libraries/net/neoforged/neoforge/<version>/ directory.
func neoForgeInstallation(s *scan, versionDir string) (Installation, bool) {
	loaderVersion := versionpkg.Version(filepath.Base(versionDir))
	tupleGameVersion, _, ok := parseModLoaderVersionTuple(
		versionDir,
		catalog.EcosystemNeoForge,
	)
	if !ok {
		return Installation{}, false
	}

	universal, hasUniversal := neoForgeArtifact(
		s,
		versionDir,
		"neoforge-"+string(loaderVersion)+"-universal.jar",
	)
	server, hasServer := neoForgeArtifact(
		s,
		versionDir,
		"neoforge-"+string(loaderVersion)+"-server.jar",
	)
	unixArgs, hasUnixArgs := neoForgeArtifact(s, versionDir, "unix_args.txt")
	winArgs, hasWinArgs := neoForgeArtifact(s, versionDir, "win_args.txt")

	universalGameVersion, universalValid := versionpkg.Unknown, false
	if hasUniversal {
		universalGameVersion, universalValid = neoForgeUniversalEvidence(
			universal,
			loaderVersion,
		)
	}
	serverGameVersion, serverReadable := versionpkg.Unknown, false
	if hasServer {
		serverGameVersion, serverReadable = neoForgeServerEvidence(server)
	}
	patched, hasPatched := neoForgePatchedServerArtifact(s, loaderVersion)
	patchedGameVersion, patchedReadable := versionpkg.Unknown, false
	if hasPatched {
		patchedGameVersion, patchedReadable = neoForgeServerEvidence(patched)
	}

	gameVersion := tupleGameVersion
	switch {
	case universalValid && hasConcreteVersion(universalGameVersion):
		gameVersion = universalGameVersion
	default:
		if argsGameVersion := neoForgeArgsGameVersion(
			s,
			versionDir,
			loaderVersion,
			hasUnixArgs,
			hasWinArgs,
		); hasConcreteVersion(argsGameVersion) {
			gameVersion = argsGameVersion
		} else if installerGameVersion := neoForgeInstallerGameVersion(
			s,
			loaderVersion,
		); hasConcreteVersion(installerGameVersion) {
			gameVersion = installerGameVersion
		} else if hasConcreteVersion(serverGameVersion) {
			gameVersion = serverGameVersion
		} else if hasConcreteVersion(patchedGameVersion) {
			gameVersion = patchedGameVersion
		}
	}

	unixArgsValid := hasUnixArgs &&
		loaderArgsLaunchValid(unixArgs, loaderVersion, gameVersion)
	winArgsValid := hasWinArgs &&
		loaderArgsLaunchValid(winArgs, loaderVersion, gameVersion)
	serverRunnable := serverReadable && artifactHasLaunchMainClass(server) &&
		verifyNeoForgeServerArtifact(server, loaderVersion)
	universalRunnable := universalValid && artifactHasLaunchMainClass(universal)

	entries := make([]*artifact, 0, 2)
	switch {
	case (unixArgsValid || winArgsValid) &&
		(universalValid || serverReadable || patchedReadable):
		// The generated argument sidecar is the Java launch target for an
		// installed NeoForge server.
		if unixArgsValid {
			entries = append(entries, unixArgs)
		}
		if winArgsValid {
			entries = append(entries, winArgs)
		}
	case serverRunnable:
		entries = append(entries, server)
	case universalRunnable:
		// A universal jar is bootable only when it declares its own launch
		// entry class; otherwise it is a runtime library.
		entries = append(entries, universal)
	default:
		return Installation{}, false
	}

	installation := NewInstallation(
		neoForgeIdentity(gameVersion, loaderVersion),
		entries[0].Path(),
	)
	for _, entry := range entries[1:] {
		installation.AddFile(entry.Path(), RoleEntryPoint)
	}
	if universalValid && !containsArtifact(entries, universal) {
		installation.AddFile(universal.Path(), RoleLibrary)
	}
	if serverReadable && !containsArtifact(entries, server) {
		installation.AddFile(server.Path(), RolePayload)
	}
	if patchedReadable && !containsArtifact(entries, patched) {
		installation.AddFile(patched.Path(), RolePayload)
	}
	if hasUnixArgs && !unixArgsValid {
		installation.AddFile(unixArgs.Path(), RoleConfiguration)
	}
	if hasWinArgs && !winArgsValid {
		installation.AddFile(winArgs.Path(), RoleConfiguration)
	}
	addMatchingMinecraftPayload(s, &installation, gameVersion)
	addMinecraftServerLibraryPayload(s, &installation, gameVersion)
	return installation, true
}

func neoForgeArtifact(
	s *scan,
	versionDir string,
	name string,
) (*artifact, bool) {
	file, ok := s.File(filepath.Join(versionDir, name))
	if !ok || file.IsDirectory() {
		return nil, false
	}
	return file, true
}

// neoForgePatchedServerArtifact resolves the patched Minecraft server jar
// that current NeoForge installers place under their own Maven coordinate.
func neoForgePatchedServerArtifact(
	s *scan,
	loaderVersion versionpkg.Version,
) (*artifact, bool) {
	version := string(loaderVersion)
	path := filepath.Join(
		s.root,
		filepath.FromSlash(neoForgePatchedServerRoot),
		version,
		"minecraft-server-patched-"+version+".jar",
	)
	file, ok := s.File(path)
	if !ok || file.IsDirectory() {
		return nil, false
	}
	return file, true
}

// verifyNeoForgeServerArtifact reports whether a server jar is positively a
// NeoForge artifact for this layout. A vanilla server jar placed in a
// Maven-shaped NeoForge directory must not become a NeoForge runtime merely
// because its name and Main-Class are present.
func verifyNeoForgeServerArtifact(
	file *artifact,
	loaderVersion versionpkg.Version,
) bool {
	archive, err := file.Archive()
	if err != nil {
		return false
	}
	if installerArtifact(archive) {
		return false
	}
	if data, ok, err := readArchiveEntry(
		archive,
		neoForgeVersionPropertiesPath,
	); err == nil && ok {
		recorded := versionpkg.Version(
			propertiesValue(data, "neoforge_version"),
		)
		return recorded == loaderVersion
	}
	if data, ok, err := readArchiveEntry(
		archive,
		neoForgeModsTomlPath,
	); err == nil && ok {
		if modVersion, found := parseTomlModVersion(data, "neoforge"); found {
			return versionpkg.Version(modVersion) == loaderVersion
		}
	}
	return archiveManifestContains(archive, "FML-System-Mods: neoforge")
}

// neoForgeUniversalEvidence validates a NeoForge universal jar and returns
// the Minecraft version the archive records locally, when it records one.
func neoForgeUniversalEvidence(
	file *artifact,
	loaderVersion versionpkg.Version,
) (versionpkg.Version, bool) {
	archive, err := file.Archive()
	if err != nil {
		return versionpkg.Unknown, false
	}
	if installerArtifact(archive) {
		return versionpkg.Unknown, false
	}

	if data, ok, err := readArchiveEntry(
		archive,
		neoForgeVersionPropertiesPath,
	); err == nil && ok {
		recordedLoader := versionpkg.Version(
			propertiesValue(data, "neoforge_version"),
		)
		if recordedLoader != loaderVersion {
			return versionpkg.Unknown, false
		}
		gameVersion := versionpkg.Version(
			propertiesValue(data, "minecraft_version"),
		)
		if hasConcreteVersion(gameVersion) &&
			isMinecraftReleaseVersion(string(gameVersion)) {
			return gameVersion, true
		}
		return versionpkg.Unknown, true
	}

	if data, ok, err := readArchiveEntry(
		archive,
		neoForgeModsTomlPath,
	); err == nil && ok {
		if modVersion, found := parseTomlModVersion(data, "neoforge"); found {
			if versionpkg.Version(modVersion) != loaderVersion {
				return versionpkg.Unknown, false
			}
			return versionpkg.Unknown, true
		}
	}

	ok, err := verifyNeoForgeUniversalManifest(archive, loaderVersion)
	if err != nil || !ok {
		return versionpkg.Unknown, false
	}
	return versionpkg.Unknown, true
}

// neoForgeServerEvidence validates a NeoForge server jar and reads the
// Minecraft version from its embedded version spec when present.
func neoForgeServerEvidence(
	file *artifact,
) (versionpkg.Version, bool) {
	archive, err := file.Archive()
	if err != nil {
		return versionpkg.Unknown, false
	}
	if installerArtifact(archive) {
		return versionpkg.Unknown, false
	}
	data, ok, err := readArchiveEntry(archive, mojangVersionJSONEntry)
	if err != nil || !ok {
		return versionpkg.Unknown, true
	}
	gameVersion, guarded, err := mojangVersionFromJSON(data)
	if err != nil || guarded || !hasConcreteVersion(gameVersion) ||
		!isMinecraftReleaseVersion(string(gameVersion)) {
		return versionpkg.Unknown, true
	}
	return gameVersion, true
}

// neoForgeArgsGameVersion reads the Minecraft version from the generated
// argument sidecar, which is authoritative for the installed layout.
func neoForgeArgsGameVersion(
	s *scan,
	versionDir string,
	loaderVersion versionpkg.Version,
	hasUnixArgs bool,
	hasWinArgs bool,
) versionpkg.Version {
	names := make([]string, 0, 2)
	if hasUnixArgs {
		names = append(names, "unix_args.txt")
	}
	if hasWinArgs {
		names = append(names, "win_args.txt")
	}
	for _, name := range names {
		file, ok := neoForgeArtifact(s, versionDir, name)
		if !ok {
			continue
		}
		data, err := file.Read()
		if err != nil {
			continue
		}
		argsLoader, argsGame := parseLoaderArgs(data)
		if hasConcreteVersion(argsLoader) && argsLoader != loaderVersion {
			continue
		}
		if hasConcreteVersion(argsGame) &&
			isMinecraftReleaseVersion(string(argsGame)) {
			return argsGame
		}
	}
	return versionpkg.Unknown
}

// neoForgeInstallerGameVersion reads the Minecraft version from the installer
// metadata kept beside the installed layout. The installer is provisioning
// metadata, not a runtime; its version spec still names the exact game
// release the loader targets.
func neoForgeInstallerGameVersion(
	s *scan,
	loaderVersion versionpkg.Version,
) versionpkg.Version {
	installerPath := filepath.Join(
		s.root,
		"neoforge-"+string(loaderVersion)+"-installer.jar",
	)
	installer, ok := s.File(installerPath)
	if !ok || installer.IsDirectory() {
		return versionpkg.Unknown
	}
	archive, err := installer.Archive()
	if err != nil {
		return versionpkg.Unknown
	}
	data, ok, err := readArchiveEntry(archive, "version.json")
	if err != nil || !ok {
		return versionpkg.Unknown
	}

	var spec struct {
		InheritsFrom string `json:"inheritsFrom"`
		Arguments    struct {
			Game []string `json:"game"`
		} `json:"arguments"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		return versionpkg.Unknown
	}
	argsLoader, argsGame := parseLoaderArgs(
		[]byte(strings.Join(spec.Arguments.Game, " ")),
	)
	if hasConcreteVersion(argsLoader) && argsLoader != loaderVersion {
		return versionpkg.Unknown
	}
	if hasConcreteVersion(argsGame) &&
		isMinecraftReleaseVersion(string(argsGame)) {
		return argsGame
	}
	inherited := versionpkg.Version(strings.TrimSpace(spec.InheritsFrom))
	if hasConcreteVersion(inherited) &&
		isMinecraftReleaseVersion(string(inherited)) {
		return inherited
	}
	return versionpkg.Unknown
}

// parseTomlModVersion returns the version declared by one [[mods]] block in a
// NeoForge mods TOML file.
func parseTomlModVersion(data []byte, modID string) (string, bool) {
	currentID := ""
	currentVersion := ""
	flush := func() (string, bool) {
		matched := currentID == modID && currentVersion != ""
		version := currentVersion
		currentID = ""
		currentVersion = ""
		return version, matched
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[[") {
			if version, matched := flush(); matched {
				return version, true
			}
			continue
		}
		key, value, found := strings.Cut(trimmed, "=")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "modId":
			currentID = strings.Trim(strings.TrimSpace(value), `"'`)
		case "version":
			currentVersion = strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return flush()
}

// verifyNeoForgeUniversalManifest checks the two manifest markers NeoForge
// universal jars carry: the system-mod declaration and the loader classpath
// entry.
func verifyNeoForgeUniversalManifest(
	reader *zip.Reader,
	loaderVersion versionpkg.Version,
) (bool, error) {
	manifest, ok, err := readArchiveEntry(reader, "META-INF/MANIFEST.MF")
	if err != nil || !ok {
		return false, err
	}

	if strings.Contains(string(manifest), "FML-System-Mods: neoforge") {
		return true, nil
	}

	classpathEntry := "libraries/net/neoforged/neoforge/" +
		string(loaderVersion) + "/"
	return strings.Contains(string(manifest), classpathEntry), nil
}

func neoForgeIdentity(
	gameVersion versionpkg.Version,
	loaderVersion versionpkg.Version,
) Identity {
	return Identity{
		Primary: catalog.VersionedRef{
			Ecosystem: catalog.EcosystemNeoForge,
			Name:      "neoforge",
			Version:   loaderVersion,
		},
		Components: []catalog.VersionedRef{
			{
				Ecosystem: catalog.EcosystemNeoForge,
				Name:      "neoforge",
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
