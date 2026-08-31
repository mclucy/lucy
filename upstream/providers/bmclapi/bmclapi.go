package bmclapi

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/types"
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

func (provider) Id() types.SourceId {
	return types.SourceBMCLAPI
}

func (p provider) ResolveVersionSelector(
	local upstream.LocalContext,
	id types.VersionedPackageRef,
) (types.VersionedPackageRef, error) {
	switch id.Eco {
	case types.EcoMinecraft:
		return mojang.Provider.ResolveVersionSelector(local, id)
	case types.EcoForge:
		return forge.Provider.ResolveVersionSelector(local, id)
	case types.EcoNeoforge:
		return neoforge.Provider.ResolveVersionSelector(local, id)
	default:
		return id, unsupportedEcosystem(id.Eco)
	}
}

func (p provider) Fetch(
	local upstream.LocalContext,
	id types.VersionedPackageRef,
) (types.ResolvedPackage, error) {
	switch id.Eco {
	case types.EcoMinecraft:
		resolved, err := mojang.Provider.Fetch(local, id)
		if err != nil {
			return types.ResolvedPackage{}, fmt.Errorf("bmclapi: fetch minecraft metadata: %w", err)
		}
		resolved.FileUrl = minecraftBaseURL + id.Version.String() + "/server"
		return resolved, nil
	case types.EcoForge:
		resolved, err := forge.Provider.Fetch(local, id)
		if err != nil {
			return types.ResolvedPackage{}, fmt.Errorf("bmclapi: fetch forge artifact: %w", err)
		}
		resolved.FileUrl, err = mirrorMavenURL(resolved.FileUrl, forgeMavenBaseURL)
		if err != nil {
			return types.ResolvedPackage{}, fmt.Errorf("bmclapi: map forge artifact URL: %w", err)
		}
		return resolved, nil
	case types.EcoNeoforge:
		resolved, err := neoforge.Provider.Fetch(local, id)
		if err != nil {
			return types.ResolvedPackage{}, fmt.Errorf("bmclapi: fetch neoforge artifact: %w", err)
		}
		resolved.FileUrl, err = mirrorMavenURL(resolved.FileUrl, neoForgeMavenBaseURL)
		if err != nil {
			return types.ResolvedPackage{}, fmt.Errorf("bmclapi: map neoforge artifact URL: %w", err)
		}
		return resolved, nil
	default:
		return types.ResolvedPackage{}, unsupportedEcosystem(id.Eco)
	}
}

func mirrorMavenURL(fileURL, officialPrefix string) (string, error) {
	path, ok := strings.CutPrefix(fileURL, officialPrefix)
	if !ok {
		return "", fmt.Errorf("unrecognized official Maven URL %q", fileURL)
	}
	return mavenBaseURL + path, nil
}

func unsupportedEcosystem(ecosystem types.Ecosystem) error {
	return fmt.Errorf("bmclapi: unsupported ecosystem %s", ecosystem)
}
