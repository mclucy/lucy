package detector

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/types"
)

// neoForgeMavenArtifactBaseURL is the Maven base URL for NeoForge artifacts.
// Source: https://maven.neoforged.net/releases/net/neoforged/neoforge/
const neoForgeMavenArtifactBaseURL = "https://maven.neoforged.net/releases/net/neoforged/neoforge"

var neoforgeArtifactHashLookup = func(version string, artifact modLoaderArtifactKind, filePath string) (bool, error) {
	return lookupModLoaderArtifactHash(version, modLoaderCandidate{kind: artifact, path: filePath}, neoForgeMavenArtifactBaseURL)
}

// NeoForgeInstallationRuntimes scans libraries/net/neoforged/neoforge/ for installed
// NeoForge server artifacts and returns detected runtime infos.
//
// Detection order:
//  1. Maven .sha1 / .sha256 hash verification
//  2. Unpack-based content verification
//
// References:
//   - https://maven.neoforged.net/releases/net/neoforged/neoforge/
//   - https://docs.neoforged.net/user/docs/server/
//   - https://github.com/neoforged/NeoForge/blob/main/CHANGELOG.md
func NeoForgeInstallationRuntimes(workPath string) []*types.RuntimeInfo {
	spec := modLoaderInstallSpec{
		platform:       types.PlatformNeoforge,
		name:           "neoforge",
		libraryRoot:    filepath.Join("libraries", "net", "neoforged", "neoforge"),
		mavenBaseURL:   neoForgeMavenArtifactBaseURL,
		candidateNames: neoForgeCandidateNames,
		unpackVerify:   verifyNeoForgeArtifactByUnpack,
	}
	return modLoaderInstallationRuntimes(workPath, spec, neoforgeArtifactHashLookup)
}

func neoForgeCandidateNames(versionDir, version string) []modLoaderCandidate {
	return []modLoaderCandidate{
		{kind: modLoaderArtifactServer, path: filepath.Join(versionDir, fmt.Sprintf("neoforge-%s-server.jar", version))},
		{kind: modLoaderArtifactUniversal, path: filepath.Join(versionDir, fmt.Sprintf("neoforge-%s-universal.jar", version))},
		{kind: modLoaderArtifactShim, path: filepath.Join(versionDir, fmt.Sprintf("neoforge-%s-shim.jar", version))},
	}
}

func verifyNeoForgeArtifactByUnpack(
	candidate modLoaderCandidate,
	gameVersion types.RawVersion,
	loaderVersion types.RawVersion,
) (bool, error) {
	file, err := os.Open(candidate.path)
	if err != nil {
		return false, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return false, err
	}

	reader, err := zip.NewReader(file, stat.Size())
	if err != nil {
		return false, err
	}

	switch candidate.kind {
	case modLoaderArtifactUniversal:
		return verifyNeoForgeUniversalManifest(reader, loaderVersion)
	case modLoaderArtifactServer:
		return forgeHasSibling(candidate.path, "run.sh", "run.bat"), nil
	default:
		return false, nil
	}
}

func verifyNeoForgeUniversalManifest(reader *zip.Reader, loaderVersion types.RawVersion) (bool, error) {
	manifest, ok, err := readZipFile(reader, "META-INF/MANIFEST.MF")
	if err != nil || !ok {
		return false, err
	}

	if strings.Contains(manifest, "Specification-Title: neoforge") {
		return true, nil
	}

	classpathEntry := fmt.Sprintf("libraries/net/neoforged/neoforge/%s/", loaderVersion)
	return strings.Contains(manifest, classpathEntry), nil
}

func readZipFile(reader *zip.Reader, name string) (string, bool, error) {
	for _, file := range reader.File {
		if file.Name != name {
			continue
		}
		r, err := file.Open()
		if err != nil {
			return "", false, err
		}
		defer r.Close()

		data, err := io.ReadAll(r)
		if err != nil {
			return "", false, err
		}
		return string(data), true, nil
	}

	return "", false, nil
}
