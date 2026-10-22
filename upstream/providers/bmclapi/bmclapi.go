package bmclapi

import (
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

type provider struct{}

var Provider provider

func (provider) Provider() catalog.Provider {
	return catalog.ProviderBMCLAPI
}

func (p provider) ResolveVersionSelector(
	local upstream.LocalContext,
	id catalog.VersionedRef,
) (catalog.VersionedRef, error) {
	switch id.Ecosystem {
	case catalog.EcosystemMinecraft:
		return mojang.Provider.ResolveVersionSelector(local, id)
	case catalog.EcosystemForge:
		return forge.Provider.ResolveVersionSelector(local, id)
	case catalog.EcosystemNeoForge:
		return neoforge.Provider.ResolveVersionSelector(local, id)
	default:
		return id, unsupportedEcosystem(id.Ecosystem)
	}
}

func (p provider) Fetch(
	local upstream.LocalContext,
	id catalog.VersionedRef,
) (upstream.ResolvedPackage, error) {
	switch id.Ecosystem {
	case catalog.EcosystemMinecraft:
		resolved, err := mojang.Provider.Fetch(local, id)
		if err != nil {
			return upstream.ResolvedPackage{}, fmt.Errorf("bmclapi: fetch minecraft metadata: %w", err)
		}
		resolved.FileURL = minecraftBaseURL + id.Version.String() + "/server"
		return resolved, nil
	case catalog.EcosystemForge:
		resolved, err := forge.Provider.Fetch(local, id)
		if err != nil {
			return upstream.ResolvedPackage{}, fmt.Errorf("bmclapi: fetch forge artifact: %w", err)
		}
		resolved.FileURL, err = mirrorMavenURL(resolved.FileURL, forgeMavenBaseURL)
		if err != nil {
			return upstream.ResolvedPackage{}, fmt.Errorf("bmclapi: map forge artifact URL: %w", err)
		}
		return resolved, nil
	case catalog.EcosystemNeoForge:
		resolved, err := neoforge.Provider.Fetch(local, id)
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
