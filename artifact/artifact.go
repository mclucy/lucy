package artifact

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/types"
)

// Analyze extracts package metadata from an artifact file.
// It opens the file internally and routes to appropriate readers based on file extension.
// For .jar/.zip files, all readers are tried. For .pyz/.mcdr, only the MCDR reader runs.
func Analyze(filePath string, opts ...Option) ([]Info, error) {
	o := &options{}
	for _, opt := range opts {
		opt(o)
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".jar", ".zip", ".pyz", ".mcdr":
	default:
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

	var results []Info

	if ext == ".pyz" || ext == ".mcdr" {
		mcdrReader := newMcdrReader()
		results, err := mcdrReader.Read(zipReader, filePath, o.slugResolver)
		if err != nil {
			return nil, err
		}
		applySlugResolver(results, o.slugResolver)
		return results, nil
	}

	for _, r := range readers {
		infos, err := r.Read(zipReader, filePath, o.slugResolver)
		if err != nil {
			continue
		}
		results = append(results, infos...)
	}
	applySlugResolver(results, o.slugResolver)

	if jarEcosystemsConflict(results) {
		return nil, fmt.Errorf(
			"ambiguous artifact %q: packages span incompatible ecosystems",
			filePath,
		)
	}

	return results, nil
}

func applySlugResolver(results []Info, resolver SlugResolver) {
	if resolver == nil {
		return
	}

	ctx := context.Background()
	for i := range results {
		normalized, err := resolver(
			ctx,
			results[i].Ref.Eco,
			results[i].Ref.Name,
		)
		if err == nil && normalized != "" {
			results[i].Ref.Name = types.BarePackageName(normalized)
		}
	}
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
// PlatformAny artifacts are excluded from the conflict check.
func jarEcosystemsConflict(infos []Info) bool {
	var hasProxy, hasServer, hasMod bool
	for _, info := range infos {
		switch info.Ref.Eco {
		case types.EcoVelocity, types.EcoBungeecord:
			hasProxy = true
		case types.EcoBukkit, types.EcoPaper:
			hasServer = true
		case types.EcoFabric, types.EcoForge, types.EcoNeoforge:
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
