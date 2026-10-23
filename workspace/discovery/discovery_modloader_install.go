package discovery

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	versionpkg "github.com/mclucy/lucy/version"
)

type modLoaderArtifactKind string

const (
	modLoaderArtifactServer    modLoaderArtifactKind = "server"
	modLoaderArtifactUniversal modLoaderArtifactKind = "universal"
	modLoaderArtifactShim      modLoaderArtifactKind = "shim"
)

type modLoaderInstallSpec struct {
	platform       catalog.Ecosystem
	name           string
	libraryRoot    string
	mavenBaseURL   string
	candidateNames func(versionDir, version string) []modLoaderCandidate
	unpackVerify   func(
		candidate modLoaderCandidate,
		gameVersion, loaderVersion versionpkg.Version,
	) (bool, error)
}

type modLoaderCandidate struct {
	kind modLoaderArtifactKind
	path string
}

func detectModLoaderInstallFromVersionDir(
	versionDir string,
	spec modLoaderInstallSpec,
	hashLookup func(
		version string,
		artifact modLoaderArtifactKind,
		filePath string,
	) (bool, error),
) (*RuntimeEvidence, error) {
	version := filepath.Base(versionDir)
	gameVersion, loaderVersion, ok := parseModLoaderVersionTuple(
		versionDir,
		spec.platform,
	)
	if !ok {
		return nil, nil
	}

	candidates := spec.candidateNames(versionDir, version)
	for _, candidate := range candidates {
		if !fileExists(candidate.path) {
			continue
		}
		verified, err := hashLookup(version, candidate.kind, candidate.path)
		if err == nil && verified {
			if candidate.kind == modLoaderArtifactShim {
				continue
			}
			return newModLoaderRuntimeEvidence(
				spec.platform,
				spec.name,
				candidate.path,
				gameVersion,
				loaderVersion,
			), nil
		}
	}

	for _, candidate := range candidates {
		if candidate.kind == modLoaderArtifactShim || !fileExists(candidate.path) {
			continue
		}
		ok, err := spec.unpackVerify(candidate, gameVersion, loaderVersion)
		if err != nil || !ok {
			continue
		}
		return newModLoaderRuntimeEvidence(
			spec.platform,
			spec.name,
			candidate.path,
			gameVersion,
			loaderVersion,
		), nil
	}

	return nil, nil
}

var neoforgeVersionDirPattern = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.\d+)*(?:-[a-zA-Z0-9.]+)*$`)

func parseModLoaderVersionTuple(
	versionDir string,
	platform catalog.Ecosystem,
) (gameVersion, loaderVersion versionpkg.Version, ok bool) {
	name := filepath.Base(versionDir)

	switch platform {
	case catalog.EcosystemForge:
		match := forgeRuntimeVersionDirPattern.FindStringSubmatch(name)
		if match == nil {
			return versionpkg.Unknown, versionpkg.Unknown, false
		}
		return versionpkg.Version(match[1]), versionpkg.Version(match[2]), true
	case catalog.EcosystemNeoForge:
		match := neoforgeVersionDirPattern.FindStringSubmatch(name)
		if match == nil {
			return versionpkg.Unknown, versionpkg.Unknown, false
		}
		gameVersion := versionpkg.Version("1." + match[1] + "." + match[2])
		return gameVersion, versionpkg.Version(name), true
	default:
		return versionpkg.Unknown, versionpkg.Unknown, false
	}
}

func modLoaderInstallationRuntimes(
	workPath string,
	spec modLoaderInstallSpec,
	hashLookup func(
		version string,
		artifact modLoaderArtifactKind,
		filePath string,
	) (bool, error),
) []*RuntimeEvidence {
	libraryRoot := filepath.Join(workPath, spec.libraryRoot)
	entries, err := os.ReadDir(libraryRoot)
	if err != nil {
		return nil
	}

	runtimes := make([]*RuntimeEvidence, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		runtime, err := detectModLoaderInstallFromVersionDir(
			filepath.Join(
				libraryRoot,
				entry.Name(),
			), spec, hashLookup,
		)
		if err != nil || runtime == nil {
			continue
		}
		runtimes = append(runtimes, runtime)
	}
	return runtimes
}

func lookupModLoaderArtifactHash(
	version string,
	candidate modLoaderCandidate,
	mavenBaseURL string,
) (bool, error) {
	sha1URL := fmt.Sprintf(
		"%s/%s/%s.sha1",
		mavenBaseURL,
		version,
		filepath.Base(candidate.path),
	)
	if ok, err := verifyArtifactHash(
		candidate.path,
		sha1URL,
		cache.HashSHA1,
	); ok || err != nil {
		return ok, err
	}
	sha256URL := fmt.Sprintf(
		"%s/%s/%s.sha256",
		mavenBaseURL,
		version,
		filepath.Base(candidate.path),
	)
	return verifyArtifactHash(candidate.path, sha256URL, cache.HashSHA256)
}

func verifyArtifactHash(
	filePath string,
	checksumURL string,
	algo cache.HashAlgorithm,
) (bool, error) {
	data, err := cache.CachedGetBytes(
		checksumURL,
		cache.BytesRequestOptions{Kind: cache.KindMetadata, MaxBytes: 256},
	)
	if err != nil {
		return false, nil
	}
	expected := strings.TrimSpace(string(data))
	if expected == "" {
		return false, nil
	}
	actual, err := hashArtifactFile(filePath, algo)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(actual, expected), nil
}

func hashArtifactFile(filePath string, algo cache.HashAlgorithm) (
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
		return "", fmt.Errorf("unsupported artifact hash algorithm: %s", algo)
	}
}

func newModLoaderRuntimeEvidence(
	platform catalog.Ecosystem,
	name string,
	filePath string,
	gameVersion versionpkg.Version,
	loaderVersion versionpkg.Version,
) *RuntimeEvidence {
	return &RuntimeEvidence{PrimaryPath: filePath, PrimaryRuntime: &catalog.VersionedRef{
		Ecosystem: platform,
		Name:      catalog.Name(name),
		Version:   loaderVersion,
	}, RuntimeComponents: []catalog.VersionedRef{
		{
			Ecosystem: platform,
			Name:      catalog.Name(name),
			Version:   loaderVersion,
		},
		{
			Ecosystem: catalog.EcosystemMinecraft,
			Name:      "minecraft",
			Version:   gameVersion,
		},
	}}
}
