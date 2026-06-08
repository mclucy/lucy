package detector

import (
	"archive/zip"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/util"
)

const forgeMavenArtifactBaseURL = "https://maven.minecraftforge.net/net/minecraftforge/forge"

type forgeArtifactKind string

const (
	forgeArtifactServer forgeArtifactKind    = "server"
	forgeArtifactUniversal forgeArtifactKind = "universal"
	forgeArtifactShim forgeArtifactKind      = "shim"
)

var forgeArtifactHashLookup = lookupForgeArtifactHash

func ForgeInstallationRuntimes(workPath string) []*ExecutableEvidence {
	forgeLib := filepath.Join(
		workPath,
		"libraries",
		"net",
		"minecraftforge",
		"forge",
	)
	entries, err := os.ReadDir(forgeLib)
	if err != nil {
		return nil
	}

	runtimes := make([]*ExecutableEvidence, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		runtime, err := detectForgeInstallFromVersionDir(
			filepath.Join(
				forgeLib,
				entry.Name(),
			),
		)
		if err != nil || runtime == nil {
			continue
		}
		runtimes = append(runtimes, runtime)
	}

	return runtimes
}

func detectForgeInstallFromVersionDir(versionDir string) (
*ExecutableEvidence,
error,
) {
	version := filepath.Base(versionDir)
	match := forgeRuntimeVersionDirPattern.FindStringSubmatch(version)
	if match == nil {
		return nil, nil
	}

	gameVersion := types.BareVersion(match[1])
	forgeVersion := types.BareVersion(match[2])
	candidates := []struct {
		kind forgeArtifactKind
		path string
	}{
		{
			forgeArtifactServer, filepath.Join(
			versionDir,
			fmt.Sprintf("forge-%s-server.jar", version),
		),
		},
		{
			forgeArtifactUniversal, filepath.Join(
			versionDir,
			fmt.Sprintf("forge-%s-universal.jar", version),
		),
		},
		{
			forgeArtifactShim,
			filepath.Join(versionDir, fmt.Sprintf("forge-%s-shim.jar", version)),
		},
	}

	for _, candidate := range candidates {
		if !fileExists(candidate.path) {
			continue
		}
		verified, err := forgeArtifactHashLookup(
			version,
			candidate.kind,
			candidate.path,
		)
		if err == nil && verified {
			if candidate.kind == forgeArtifactShim {
				continue
			}
			return buildForgeRuntimeInfo(
				candidate.path,
				gameVersion,
				forgeVersion,
			), nil
		}
	}

	for _, candidate := range candidates[:2] {
		if !fileExists(candidate.path) {
			continue
		}
		ok, err := verifyForgeArtifactByUnpack(
			candidate.path,
			candidate.kind,
			gameVersion,
			forgeVersion,
		)
		if err != nil || !ok {
			continue
		}
		return buildForgeRuntimeInfo(
			candidate.path,
			gameVersion,
			forgeVersion,
		), nil
	}

	return nil, nil
}

func verifyForgeArtifactByUnpack(
jarPath string,
kind forgeArtifactKind,
gameVersion types.BareVersion,
forgeVersion types.BareVersion,
) (bool, error) {
	file, err := os.Open(jarPath)
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

	if kind == forgeArtifactUniversal {
		manifestForge, manifestGame := parseForgeManifest(reader)
		return manifestForge == forgeVersion && manifestGame == gameVersion, nil
	}

	if kind == forgeArtifactServer {
		if compareForgeMajor(forgeVersion, 61) >= 0 {
			shimPath := filepath.Join(
				filepath.Dir(jarPath),
				fmt.Sprintf("forge-%s-%s-shim.jar", gameVersion, forgeVersion),
			)
			if !fileExists(shimPath) {
				return false, nil
			}
			ok, err := verifyForgeShimJar(shimPath)
			if err != nil || !ok {
				return false, err
			}
		}
		return forgeHasSibling(jarPath, "unix_args.txt", "win_args.txt"), nil
	}

	return false, nil
}

func verifyForgeShimJar(jarPath string) (bool, error) {
	file, err := os.Open(jarPath)
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

	hasProperties := false
	hasList := false
	for _, f := range reader.File {
		switch f.Name {
		case "bootstrap-shim.properties":
			hasProperties = true
		case "bootstrap-shim.list":
			hasList = true
		}
	}
	return hasProperties && hasList, nil
}

func lookupForgeArtifactHash(
version string,
artifact forgeArtifactKind,
filePath string,
) (bool, error) {
	sha1URL := fmt.Sprintf(
		"%s/%s/%s.sha1",
		forgeMavenArtifactBaseURL,
		version,
		filepath.Base(filePath),
	)
	if ok, err := verifyForgeArtifactHash(
		filePath,
		sha1URL,
		cache.HashSHA1,
	); ok || err != nil {
		return ok, err
	}

	sha256URL := fmt.Sprintf(
		"%s/%s/%s.sha256",
		forgeMavenArtifactBaseURL,
		version,
		filepath.Base(filePath),
	)
	return verifyForgeArtifactHash(filePath, sha256URL, cache.HashSHA256)
}

func verifyForgeArtifactHash(
filePath string,
checksumURL string,
algo cache.HashAlgorithm,
) (bool, error) {
	data, err := util.CachedGetBytes(
		checksumURL,
		util.BytesRequestOptions{Kind: cache.KindMetadata, MaxBytes: 256},
	)
	if err != nil {
		return false, nil
	}
	expected := strings.TrimSpace(string(data))
	if expected == "" {
		return false, nil
	}
	actual, err := hashForgeArtifact(filePath, algo)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(actual, expected), nil
}

func hashForgeArtifact(filePath string, algo cache.HashAlgorithm) (
string,
error,
) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	switch algo {
	case cache.HashSHA1:
		sum := sha1.Sum(data)
		return hex.EncodeToString(sum[:]), nil
	case cache.HashSHA256:
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:]), nil
	default:
		return "", fmt.Errorf(
			"unsupported forge artifact hash algorithm: %s",
			algo,
		)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
