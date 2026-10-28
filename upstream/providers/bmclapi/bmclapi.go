package bmclapi

import (
	"context"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/providers/forge"
	"github.com/mclucy/lucy/upstream/providers/mojang"
	"github.com/mclucy/lucy/upstream/providers/neoforge"
)

const (
	minecraftBaseURL = "https://bmclapi2.bangbang93.com/version/"
	mavenBaseURL     = "https://bmclapi2.bangbang93.com/maven/"

	forgeMavenBaseURL    = "https://maven.minecraftforge.net/"
	neoForgeMavenBaseURL = "https://maven.neoforged.net/releases/"
)

// Provider maps official server artifacts onto the BMCLAPI mirror. It resolves
// versions through the official providers it was constructed with, so
// floating selectors and published digests keep their canonical meaning.
type Provider struct {
	mojang   *mojang.Provider
	forge    *forge.Provider
	neoforge *neoforge.Provider
}

// New constructs a BMCLAPI mirror provider over the official providers whose
// artifacts it mirrors.
func New(
	mojangProvider *mojang.Provider,
	forgeProvider *forge.Provider,
	neoForgeProvider *neoforge.Provider,
) *Provider {
	return &Provider{
		mojang:   mojangProvider,
		forge:    forgeProvider,
		neoforge: neoForgeProvider,
	}
}

func (p *Provider) Provider() catalog.Provider {
	return catalog.ProviderBMCLAPI
}

func (p *Provider) ResolveVersionSelector(
	ctx context.Context,
	local upstream.LocalContext,
	id catalog.VersionedRef,
) (catalog.VersionedRef, error) {
	switch id.Ecosystem {
	case catalog.EcosystemMinecraft:
		return p.mojang.ResolveVersionSelector(ctx, local, id)
	case catalog.EcosystemForge:
		return p.forge.ResolveVersionSelector(ctx, local, id)
	case catalog.EcosystemNeoForge:
		return p.neoforge.ResolveVersionSelector(ctx, local, id)
	default:
		return id, unsupportedEcosystem(id.Ecosystem)
	}
}

func (p *Provider) Fetch(
	ctx context.Context,
	local upstream.LocalContext,
	id catalog.VersionedRef,
) (upstream.ResolvedPackage, error) {
	switch id.Ecosystem {
	case catalog.EcosystemMinecraft:
		resolved, err := p.mojang.Fetch(ctx, local, id)
		if err != nil {
			return upstream.ResolvedPackage{}, fmt.Errorf("bmclapi: fetch minecraft metadata: %w", err)
		}
		resolved.FileURL = minecraftBaseURL + id.Version.String() + "/server"
		return resolved, nil
	case catalog.EcosystemForge:
		resolved, err := p.forge.Fetch(ctx, local, id)
		if err != nil {
			return upstream.ResolvedPackage{}, fmt.Errorf("bmclapi: fetch forge artifact: %w", err)
		}
		resolved.FileURL, err = mirrorMavenURL(resolved.FileURL, forgeMavenBaseURL)
		if err != nil {
			return upstream.ResolvedPackage{}, fmt.Errorf("bmclapi: map forge artifact URL: %w", err)
		}
		return resolved, nil
	case catalog.EcosystemNeoForge:
		resolved, err := p.neoforge.Fetch(ctx, local, id)
		if err != nil {
			return upstream.ResolvedPackage{}, fmt.Errorf("bmclapi: fetch neoforge artifact: %w", err)
		}
		resolved.FileURL, err = mirrorMavenURL(resolved.FileURL, neoForgeMavenBaseURL)
		if err != nil {
			return upstream.ResolvedPackage{}, fmt.Errorf("bmclapi: map neoforge artifact URL: %w", err)
		}
		return resolved, nil
	default:
		return upstream.ResolvedPackage{}, unsupportedEcosystem(id.Ecosystem)
	}
}

func mirrorMavenURL(fileURL, officialPrefix string) (string, error) {
	path, ok := strings.CutPrefix(fileURL, officialPrefix)
	if !ok {
		return "", fmt.Errorf("unrecognized official Maven URL %q", fileURL)
	}
	return mavenBaseURL + path, nil
}

func unsupportedEcosystem(ecosystem catalog.Ecosystem) error {
	return fmt.Errorf("bmclapi: unsupported ecosystem %s", ecosystem)
}
