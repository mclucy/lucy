package discovery

import (
	"archive/zip"
	"strings"

	versionpkg "github.com/mclucy/lucy/version"
)

// loaderArgsLaunchValid reports whether a generated argument sidecar is a
// positively identified launch target for this loader layout: it names the
// same loader and Minecraft versions and either a server launch target or a
// server main class. NeoForge 26.2 dropped --launchTarget in favor of
// net.neoforged.fml.startup.Server.
func loaderArgsLaunchValid(
	file *artifact,
	loaderVersion versionpkg.Version,
	gameVersion versionpkg.Version,
) bool {
	if file == nil || !hasConcreteVersion(gameVersion) {
		return false
	}
	data, err := file.Read()
	if err != nil {
		return false
	}
	argsLoader, argsGame := parseLoaderArgs(data)
	if hasConcreteVersion(argsLoader) && argsLoader != loaderVersion {
		return false
	}
	if !hasConcreteVersion(argsGame) || argsGame != gameVersion {
		return false
	}
	target := strings.ToLower(parseLaunchTarget(data))
	if target != "" && strings.Contains(target, "server") {
		return true
	}
	mainClass := strings.ToLower(parseLaunchMainClass(data))
	return mainClass != "" && strings.Contains(mainClass, "server")
}

// parseLaunchMainClass returns the first fully qualified Java class token in a
// generated argument sidecar. Flags, JVM properties, and classpath entries are
// not class tokens.
func parseLaunchMainClass(data []byte) string {
	for line := range strings.SplitSeq(string(data), "\n") {
		candidate := strings.TrimSpace(line)
		if candidate == "" || strings.HasPrefix(candidate, "-") ||
			strings.ContainsAny(candidate, " \t=/:") {
			continue
		}
		if isJavaClassName(candidate) {
			return candidate
		}
	}
	return ""
}

func isJavaClassName(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for index, r := range part {
			switch {
			case r == '_' || r == '$':
			case r >= 'a' && r <= 'z':
			case r >= 'A' && r <= 'Z':
			case index > 0 && r >= '0' && r <= '9':
			default:
				return false
			}
		}
	}
	last := parts[len(parts)-1]
	return last[0] >= 'A' && last[0] <= 'Z'
}

// artifactHasLaunchMainClass reports whether a jar declares a Main-Class and
// is therefore independently java -jar runnable.
func artifactHasLaunchMainClass(file *artifact) bool {
	if file == nil {
		return false
	}
	archive, err := file.Archive()
	if err != nil {
		return false
	}
	mainClass, ok := readArchiveManifestMainClass(archive)
	return ok && strings.TrimSpace(mainClass) != ""
}

// forgeProductContent reports whether a jar carries positive Forge content.
// Legacy universal jars have no implementation versions in their manifest;
// they identify themselves through Tweak-Class, a Forge main class, or FML
// classes.
func forgeProductContent(zipReader *zip.Reader) bool {
	switch {
	case archiveManifestContains(zipReader, "FML-System-Mods: forge"):
		return true
	case archiveManifestContains(zipReader, "Tweak-Class: net.minecraftforge."):
		return true
	case archiveManifestContains(zipReader, "Main-Class: net.minecraftforge."):
		return true
	case archiveContains(
		zipReader,
		"net/minecraftforge/fml/relauncher/ServerLaunchWrapper.class",
	):
		return true
	case archiveContains(
		zipReader,
		"net/minecraftforge/fml/common/launcher/FMLTweaker.class",
	):
		return true
	default:
		return false
	}
}

// verifyForgeUniversalArtifact validates a universal jar by the Forge and
// Minecraft versions recorded in its manifest. The directory name supplies
// the Minecraft version when the manifest does not repeat it. Legacy
// universals without implementation versions are accepted only with positive
// Forge content in the archive.
func verifyForgeUniversalArtifact(
	file *artifact,
	forgeVersion versionpkg.Version,
	gameVersion versionpkg.Version,
) bool {
	archive, err := file.Archive()
	if err != nil {
		return false
	}
	if installerArtifact(archive) {
		return false
	}
	manifestForge, manifestGame := parseForgeManifest(archive)
	if hasConcreteVersion(manifestForge) {
		if manifestForge != forgeVersion {
			return false
		}
		if !hasConcreteVersion(manifestGame) {
			manifestGame = gameVersion
		}
		return manifestGame == gameVersion
	}
	return forgeProductContent(archive)
}

// verifyForgeServerArtifact reports whether a server jar is positively a
// Forge artifact for this layout. A vanilla server jar placed in a
// Maven-shaped Forge directory must not become a Forge runtime merely because
// its name and Main-Class are present.
func verifyForgeServerArtifact(
	file *artifact,
	forgeVersion versionpkg.Version,
	gameVersion versionpkg.Version,
) bool {
	archive, err := file.Archive()
	if err != nil {
		return false
	}
	if installerArtifact(archive) {
		return false
	}
	manifestForge, manifestGame := parseForgeManifest(archive)
	if hasConcreteVersion(manifestForge) {
		if manifestForge != forgeVersion {
			return false
		}
		if hasConcreteVersion(manifestGame) && manifestGame != gameVersion {
			return false
		}
		return true
	}
	return forgeProductContent(archive)
}

// installerArtifact reports whether a jar is a loader installer rather than
// a runtime artifact.
func installerArtifact(zipReader *zip.Reader) bool {
	if archiveContains(zipReader, "install_profile.json") {
		return true
	}
	data, ok, err := readArchiveEntry(zipReader, "version.json")
	if err != nil || !ok {
		return false
	}
	_, guarded, err := mojangVersionFromJSON(data)
	return err == nil && guarded
}

// archiveContains reports whether a ZIP archive has an entry with this exact
// name.
func archiveContains(zipReader *zip.Reader, name string) bool {
	for _, entry := range zipReader.File {
		if entry.Name == name {
			return true
		}
	}
	return false
}

// archiveManifestContains reports whether the jar manifest contains needle.
func archiveManifestContains(zipReader *zip.Reader, needle string) bool {
	data, ok, err := readArchiveEntry(zipReader, "META-INF/MANIFEST.MF")
	return err == nil && ok && strings.Contains(string(data), needle)
}

// verifyForgeShimArtifact requires the two bootstrap descriptors that make a
// Forge shim a launch target rather than an arbitrary jar named *-shim.jar.
func verifyForgeShimArtifact(file *artifact) bool {
	archive, err := file.Archive()
	if err != nil {
		return false
	}
	hasProperties := false
	hasList := false
	for _, entry := range archive.File {
		switch entry.Name {
		case "bootstrap-shim.properties":
			hasProperties = true
		case "bootstrap-shim.list":
			hasList = true
		}
	}
	return hasProperties && hasList
}
