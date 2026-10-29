package discovery

import (
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/catalog"
	versionpkg "github.com/mclucy/lucy/version"
)

const forgeLibraryRoot = "libraries/net/minecraftforge/forge"

// discoverForge recognizes installed Forge servers under the Forge Maven
// library directory. One directory holds at most one Forge installation:
// modern layouts keep the runtime in a universal library and launch it
// through a generated argument file or a bootstrap shim; legacy layouts use
// the universal jar directly. The installer jar itself is a provisioning
// tool and is never a runtime.
func discoverForge(s *scan) ([]Installation, []Finding) {
	if s == nil {
		return nil, nil
	}

	root := filepath.Join(s.root, filepath.FromSlash(forgeLibraryRoot))
	installations := make([]Installation, 0)
	if entries, err := s.ReadDir(root); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			installation, ok := forgeInstallation(
				s,
				filepath.Join(root, entry.Name()),
			)
			if ok {
				installations = append(installations, installation)
			}
		}
	}
	installations = append(
		installations,
		discoverRootForgeInstallations(s)...,
	)
	return installations, nil
}

// discoverRootForgeInstallations recognizes standalone Forge launchers kept
// directly in the server directory. These are the legacy universal and server
// jars that predate the Maven-shaped installed layout. Installers and shims
// are provisioning/launch fragments, not standalone installations.
func discoverRootForgeInstallations(s *scan) []Installation {
	installations := make([]Installation, 0)
	for _, file := range s.RootFiles() {
		if file.IsDirectory() || !isJarArtifact(file) {
			continue
		}
		installation, ok := rootForgeInstallation(s, file)
		if ok {
			installations = append(installations, installation)
		}
	}
	return installations
}

// rootForgeInstallation builds one installation from a root-level Forge
// launcher. Identity comes from manifest metadata, the Maven-shaped filename,
// or validated argument sidecars. A jar without positive Forge content is
// never claimed, even when it has a Main-Class.
func rootForgeInstallation(
	s *scan,
	file *artifact,
) (Installation, bool) {
	archive, err := file.Archive()
	if err != nil {
		return Installation{}, false
	}
	if verifyForgeShimArtifact(file) {
		return Installation{}, false
	}
	if installerArtifact(archive) {
		return Installation{}, false
	}

	manifestForge, manifestGame := parseForgeManifest(archive)
	tupleGame, tupleForge, _ := parseForgeVersionTupleFromPath(file.Path())

	unixArgs, hasUnixArgs := s.Sibling(file, "unix_args.txt")
	winArgs, hasWinArgs := s.Sibling(file, "win_args.txt")
	argsLoader, argsGame := versionpkg.Unknown, versionpkg.Unknown
	if hasUnixArgs {
		if data, err := unixArgs.Read(); err == nil {
			argsLoader, argsGame = parseLoaderArgs(data)
		}
	}
	if !hasConcreteVersion(argsGame) && hasWinArgs {
		if data, err := winArgs.Read(); err == nil {
			argsLoader, argsGame = parseLoaderArgs(data)
		}
	}

	gameVersion := firstConcreteVersion(manifestGame, tupleGame, argsGame)
	forgeVersion := firstConcreteVersion(manifestForge, tupleForge, argsLoader)
	if !hasConcreteVersion(gameVersion) || !hasConcreteVersion(forgeVersion) {
		return Installation{}, false
	}

	unixArgsValid := hasUnixArgs &&
		loaderArgsLaunchValid(unixArgs, forgeVersion, gameVersion)
	winArgsValid := hasWinArgs &&
		loaderArgsLaunchValid(winArgs, forgeVersion, gameVersion)

	base := filepath.Base(file.Path())
	kind := forgeArtifactKindFromName(base)
	universalSibling, hasUniversalSibling := forgeRootSibling(s, file, base, "universal")
	serverSibling, hasServerSibling := forgeRootSibling(s, file, base, "server")
	shim, hasShim := forgeRootSibling(s, file, base, "shim")

	universalValid := kind == "universal" &&
		verifyForgeUniversalArtifact(file, forgeVersion, gameVersion)
	if !universalValid && hasUniversalSibling {
		universalValid = verifyForgeUniversalArtifact(
			universalSibling,
			forgeVersion,
			gameVersion,
		)
	}
	serverReadable := (kind == "server" && forgeArtifactReadable(file)) ||
		(hasServerSibling && forgeArtifactReadable(serverSibling))
	fileReadable := forgeArtifactReadable(file)
	shimValid := hasShim && verifyForgeShimArtifact(shim)
	runnable := artifactHasLaunchMainClass(file)
	manifestMatches := hasConcreteVersion(manifestForge) &&
		manifestForge == forgeVersion &&
		(!hasConcreteVersion(manifestGame) || manifestGame == gameVersion)
	productContent := manifestMatches || forgeProductContent(archive)
	runtimePresent := universalValid || serverReadable || fileReadable

	entries := make([]*artifact, 0, 3)
	switch {
	case shimValid && runtimePresent:
		entries = append(entries, shim)
		if unixArgsValid {
			entries = append(entries, unixArgs)
		}
		if winArgsValid {
			entries = append(entries, winArgs)
		}
	case (unixArgsValid || winArgsValid) && runtimePresent:
		if unixArgsValid {
			entries = append(entries, unixArgs)
		}
		if winArgsValid {
			entries = append(entries, winArgs)
		}
	case runnable && productContent:
		entries = append(entries, file)
	default:
		return Installation{}, false
	}

	installation := NewInstallation(
		forgeIdentity(gameVersion, forgeVersion),
		entries[0].Path(),
	)
	for _, entry := range entries[1:] {
		installation.AddFile(entry.Path(), RoleEntryPoint)
	}

	universalArtifact := universalSibling
	if kind == "universal" {
		universalArtifact = file
	}
	serverArtifact := serverSibling
	if kind == "server" {
		serverArtifact = file
	}
	if universalValid && universalArtifact != nil &&
		!containsArtifact(entries, universalArtifact) {
		installation.AddFile(universalArtifact.Path(), RoleLibrary)
	}
	if serverReadable && serverArtifact != nil &&
		!containsArtifact(entries, serverArtifact) {
		installation.AddFile(serverArtifact.Path(), RolePayload)
	}
	if !containsArtifact(entries, file) && kind == "" && fileReadable {
		installation.AddFile(file.Path(), RolePayload)
	}
	if hasUnixArgs && !unixArgsValid {
		installation.AddFile(unixArgs.Path(), RoleConfiguration)
	}
	if hasWinArgs && !winArgsValid {
		installation.AddFile(winArgs.Path(), RoleConfiguration)
	}
	if shimValid {
		addForgeClassPathFiles(s, &installation, shim)
	}
	addForgeRootClassPathFiles(s, &installation, file, gameVersion)
	addMatchingMinecraftPayload(s, &installation, gameVersion)
	addMinecraftServerLibraryPayload(s, &installation, gameVersion)
	return installation, true
}

// addForgeRootClassPathFiles claims the local libraries and Minecraft server
// payload named by a root Forge launcher manifest. Legacy universals list the
// Minecraft server jar directly in Class-Path.
func addForgeRootClassPathFiles(
	s *scan,
	installation *Installation,
	jar *artifact,
	gameVersion versionpkg.Version,
) {
	for _, entry := range parseArchiveClassPathFromArtifact(jar) {
		entry = strings.Trim(strings.TrimSpace(entry), `"`)
		if entry == "" || strings.Contains(entry, "://") ||
			strings.HasPrefix(entry, "file:") {
			continue
		}
		path := filepath.FromSlash(entry)
		candidates := []string{filepath.Join(s.root, path)}
		if filepath.IsAbs(path) {
			candidates = []string{path}
		} else {
			candidates = append(
				candidates,
				filepath.Join(filepath.Dir(jar.Path()), path),
			)
		}
		for _, candidate := range candidates {
			file, ok := s.File(candidate)
			if !ok || file.IsDirectory() {
				continue
			}
			role := RoleLibrary
			base := strings.ToLower(filepath.Base(file.Path()))
			if strings.Contains(base, "minecraft_server") ||
				(strings.HasPrefix(base, "server-") &&
					strings.HasSuffix(base, ".jar")) {
				if recorded := parseServerJarGameVersion(file); recorded == versionpkg.Unknown ||
					recorded == gameVersion {
					role = RolePayload
				}
			}
			installation.AddFile(file.Path(), role)
			break
		}
	}
}

func forgeRootSibling(
	s *scan,
	file *artifact,
	base string,
	kind string,
) (*artifact, bool) {
	prefix := forgeArtifactNamePrefix(base)
	if prefix == "" {
		return nil, false
	}
	sibling, ok := s.Sibling(file, prefix+"-"+kind+".jar")
	if !ok || sibling.IsDirectory() {
		return nil, false
	}
	return sibling, true
}

func forgeArtifactNamePrefix(base string) string {
	for _, kind := range []string{"server", "universal", "shim"} {
		suffix := "-" + kind + ".jar"
		if strings.HasSuffix(base, suffix) {
			return strings.TrimSuffix(base, suffix)
		}
	}
	return ""
}

func forgeArtifactKindFromName(base string) string {
	for _, kind := range []string{"server", "universal", "shim"} {
		if strings.HasSuffix(base, "-"+kind+".jar") {
			return kind
		}
	}
	return ""
}

func firstConcreteVersion(values ...versionpkg.Version) versionpkg.Version {
	for _, value := range values {
		if hasConcreteVersion(value) {
			return value
		}
	}
	return versionpkg.Unknown
}

// forgeInstallation builds one installation from a
// libraries/net/minecraftforge/forge/<minecraft>-<forge>/ directory. A shim
// is the real entry point when it is complete; a server jar with generated
// arguments is the next choice; the universal jar is the entry point only
// for layouts that predate both, and is otherwise a runtime library.
func forgeInstallation(s *scan, versionDir string) (Installation, bool) {
	version := filepath.Base(versionDir)
	match := forgeRuntimeVersionDirPattern.FindStringSubmatch(version)
	if match == nil {
		return Installation{}, false
	}
	gameVersion := versionpkg.Version(match[1])
	forgeVersion := versionpkg.Version(match[2])

	universal, hasUniversal := forgeArtifact(
		s,
		versionDir,
		"forge-"+version+"-universal.jar",
	)
	server, hasServer := forgeArtifact(
		s,
		versionDir,
		"forge-"+version+"-server.jar",
	)
	shim, hasShim := forgeArtifact(
		s,
		versionDir,
		"forge-"+version+"-shim.jar",
	)
	unixArgs, hasUnixArgs := forgeArtifact(s, versionDir, "unix_args.txt")
	winArgs, hasWinArgs := forgeArtifact(s, versionDir, "win_args.txt")

	shimValid := hasShim && verifyForgeShimArtifact(shim)
	serverReadable := hasServer && forgeArtifactReadable(server)
	universalValid := hasUniversal &&
		verifyForgeUniversalArtifact(universal, forgeVersion, gameVersion)
	unixArgsValid := hasUnixArgs &&
		loaderArgsLaunchValid(unixArgs, forgeVersion, gameVersion)
	winArgsValid := hasWinArgs &&
		loaderArgsLaunchValid(winArgs, forgeVersion, gameVersion)
	serverRunnable := serverReadable && artifactHasLaunchMainClass(server) &&
		verifyForgeServerArtifact(server, forgeVersion, gameVersion)
	universalRunnable := universalValid && artifactHasLaunchMainClass(universal)

	entries := make([]*artifact, 0, 3)
	switch {
	case shimValid && (universalValid || serverReadable):
		// The shim is the direct java -jar launch target, but it only
		// bootstraps a complete layout. Generated arguments remain launch
		// targets as well when they validate.
		entries = append(entries, shim)
		if unixArgsValid {
			entries = append(entries, unixArgs)
		}
		if winArgsValid {
			entries = append(entries, winArgs)
		}
	case (unixArgsValid || winArgsValid) && (universalValid || serverReadable):
		if unixArgsValid {
			entries = append(entries, unixArgs)
		}
		if winArgsValid {
			entries = append(entries, winArgs)
		}
	case serverRunnable:
		entries = append(entries, server)
	case universalRunnable:
		// Only genuine legacy universal jars with their own launch entry
		// class are bootable; otherwise the universal is a runtime library.
		entries = append(entries, universal)
	default:
		// An installer, a library-only universal, or a bare argument file
		// is not a runnable layout.
		return Installation{}, false
	}

	installation := NewInstallation(
		forgeIdentity(gameVersion, forgeVersion),
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
	if hasUnixArgs && !unixArgsValid {
		installation.AddFile(unixArgs.Path(), RoleConfiguration)
	}
	if hasWinArgs && !winArgsValid {
		installation.AddFile(winArgs.Path(), RoleConfiguration)
	}
	if shimValid {
		addForgeClassPathFiles(s, &installation, shim)
	}
	addMatchingMinecraftPayload(s, &installation, gameVersion)
	addMinecraftServerLibraryPayload(s, &installation, gameVersion)
	return installation, true
}

func containsArtifact(artifacts []*artifact, target *artifact) bool {
	for _, artifact := range artifacts {
		if artifact == target {
			return true
		}
	}
	return false
}

// addForgeClassPathFiles claims the libraries named by a Forge shim
// manifest. Forge writes those entries relative to the server root, so the
// root is tried before the shim's own directory.
func addForgeClassPathFiles(
	s *scan,
	installation *Installation,
	shim *artifact,
) {
	for _, entry := range parseArchiveClassPathFromArtifact(shim) {
		entry = strings.Trim(strings.TrimSpace(entry), `"`)
		if entry == "" || strings.Contains(entry, "://") ||
			strings.HasPrefix(entry, "file:") {
			continue
		}
		path := filepath.FromSlash(entry)
		candidates := []string{filepath.Join(s.root, path)}
		if filepath.IsAbs(path) {
			candidates = []string{path}
		} else {
			candidates = append(
				candidates,
				filepath.Join(filepath.Dir(shim.Path()), path),
			)
		}
		for _, candidate := range candidates {
			file, ok := s.File(candidate)
			if !ok || file.IsDirectory() {
				continue
			}
			installation.AddFile(file.Path(), RoleLibrary)
			break
		}
	}
}

func forgeArtifact(
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

// forgeArtifactReadable reports whether the artifact is a readable archive.
func forgeArtifactReadable(file *artifact) bool {
	_, err := file.Archive()
	return err == nil
}

func parseArchiveClassPathFromArtifact(file *artifact) []string {
	archive, err := file.Archive()
	if err != nil {
		return nil
	}
	return parseArchiveClassPath(archive)
}

func forgeIdentity(
	gameVersion versionpkg.Version,
	forgeVersion versionpkg.Version,
) Identity {
	return Identity{
		Primary: catalog.VersionedRef{
			Ecosystem: catalog.EcosystemForge,
			Name:      "forge",
			Version:   forgeVersion,
		},
		Components: []catalog.VersionedRef{
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
		},
	}
}
