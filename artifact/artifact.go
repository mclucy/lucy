package artifact

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/types"
)

// SupportsPath reports whether path has a supported artifact archive
// extension.
func SupportsPath(path string) bool {
	return supportedExt(strings.ToLower(filepath.Ext(path)))
}

func supportedExt(ext string) bool {
	switch ext {
	case ".jar", ".zip", ".pyz", ".mcdr":
		return true
	default:
		return false
	}
}

// ReadInfo extracts package metadata from an artifact file. For .jar/.zip
// files all readers are tried; for .pyz/.mcdr only the MCDR reader runs.
func ReadInfo(filePath string, opts ...Option) ([]PackageInfo, error) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}
	_ = o

	ext := strings.ToLower(filepath.Ext(filePath))
	if !supportedExt(ext) {
		return nil, fmt.Errorf("unsupported artifact format: %s", ext)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat artifact: %w", err)
	}

	zipReader, err := zip.NewReader(file, stat.Size())
	if err != nil {
		return nil, fmt.Errorf("read zip: %w", err)
	}

	var results []PackageInfo

	if ext == ".pyz" || ext == ".mcdr" {
		mcdrReader := newMcdrReader()
		results, err := mcdrReader.Read(zipReader, filePath)
		if err != nil {
			return nil, err
		}
		return results, nil
	}

	for _, r := range readers {
		infos, err := r.Read(zipReader, filePath)
		if err != nil {
			continue
		}
		results = append(results, infos...)
	}
	if jarEcosystemsConflict(results) {
		return nil, fmt.Errorf(
			"ambiguous artifact %q: packages span incompatible ecosystems",
			filePath,
		)
	}

	return results, nil
}

// jarEcosystemsConflict returns true when the detected artifacts span two or more
// ecosystem families that cannot coexist in a single deployable JAR.
//
// Ecosystem families:
//
//	proxyFamily  – velocity, bungeecord
//	serverFamily – bukkit, paper
//	modFamily    – fabric, forge, neoforge
//
// Artifacts whose ecosystem is types.EcosystemUnspecified are excluded from
// the conflict check.
func jarEcosystemsConflict(infos []PackageInfo) bool {
	var hasProxy, hasServer, hasMod bool
	for _, info := range infos {
		switch info.Ref.Ecosystem {
		case types.EcosystemVelocity, types.EcosystemBungeeCord:
			hasProxy = true
		case types.EcosystemBukkit, types.EcosystemPaper:
			hasServer = true
		case types.EcosystemFabric, types.EcosystemForge, types.EcosystemNeoForge:
			hasMod = true
		}
	}

	families := 0
	if hasProxy {
		families++
	}
	if hasServer {
		families++
	}
	if hasMod {
		families++
	}
	return families > 1
}
