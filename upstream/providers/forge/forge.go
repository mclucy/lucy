package forge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
)

const (
	docsURL               = "https://files.minecraftforge.net/"
	promotionsURL         = "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json"
	mavenBaseURL          = "https://maven.minecraftforge.net/net/minecraftforge/forge"
	neoForgeComparisonURL = "https://docs.neoforged.net/user/docs/server"
)

type provider struct{}

var Provider provider

func (provider) Id() types.SourceId {
	return types.SourceForge
}

type promotions struct {
	Promos map[string]string `json:"promos"`
}

func (p provider) ResolveVersionSelector(id types.VersionedPackageRef) (
	types.VersionedPackageRef,
	error,
) {
	gameVersion, err := minecraftVersionForInstall()
	if err != nil {
		return id, err
	}

	forgeVersion, err := forgeVersionFromPackageRef(id, gameVersion)
	if err != nil {
		return id, err
	}

	return types.VersionedPackageRef{
		PackageRef: types.PackageRef{
			Platform: types.PlatformForge,
			Name:     "forge",
		},
		Version: types.BareVersion(forgeVersion),
	}, nil
}

func (p provider) Fetch(id types.VersionedPackageRef) (types.ResolvedPackage, error) {
	gameVersion, err := minecraftVersionForInstall()
	if err != nil {
		return types.ResolvedPackage{}, err
	}

	if gameVersion == types.VersionUnknown {
		return types.ResolvedPackage{}, fmt.Errorf(
			"unknown minecraft version, cannot infer forge bootstrap artifact; see %s",
			docsURL,
		)
	}

	forgeVersion, err := forgeVersionFromPackageRef(id, gameVersion)
	if err != nil {
		return types.ResolvedPackage{}, err
	}

	fileURL := installerURL(gameVersion, forgeVersion)

	return types.ResolvedPackage{
		FileUrl: fileURL,
		Filename: fmt.Sprintf(
			"forge-%s-%s-installer.jar",
			gameVersion.String(),
			forgeVersion,
		),
	}, nil
}

func minecraftVersionForInstall() (types.BareVersion, error) {
	serverInfo := workspace.ServerInfo()
	switch serverInfo.DerivedModLoader() {
	case types.PlatformVanilla:
		return serverInfo.Runtime.GameVersion, nil
	case types.PlatformNone:
		selectedVersion := promptSelectMinecraftVersion()
		if selectedVersion == "none" || selectedVersion == "error" {
			return "", errors.New("minecraft version selection cancelled or failed")
		}
		return types.BareVersion(selectedVersion), nil
	default:
		return serverInfo.Runtime.GameVersion, nil
	}
}

func forgeVersionFromPackageRef(
	p types.VersionedPackageRef,
	gameVersion types.BareVersion,
) (string, error) {
	if p.Version != types.VersionLatest &&
		p.Version != types.VersionCompatible &&
		p.Version != types.VersionAny &&
		p.Version != types.VersionUnknown {
		return p.Version.String(), nil
	}
	return fetchVersion(gameVersion)
}

func promptSelectMinecraftVersion() (version string) {
	versions, err := fetchSupportedMinecraftVersions()
	if err != nil || len(versions) == 0 {
		return "error"
	}

	gameVersions := versions

	var installLatest bool
	options := huh.NewOptions(gameVersions...)
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("No current Minecraft installation found.").
				Description("Do you want to install forge with its latest supported Minecraft version?").
				Affirmative("Yes, proceed").
				Negative("No, select a game version").
				Value(&installLatest),
		),
	).Run()
	if err != nil {
		return "none"
	}
	if installLatest {
		return gameVersions[len(gameVersions)-1]
	}
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a Minecraft installation").
				Options(options...).
				Filtering(true).
				Height(10).
				Value(&version),
		).WithHide(installLatest),
	).Run()
	if err != nil {
		return "none"
	}
	return
}

func fetchSupportedMinecraftVersions() ([]string, error) {
	data, err := cache.CachedGetBytes(
		promotionsURL,
		cache.BytesRequestOptions{Kind: cache.KindMetadata},
	)
	if err != nil {
		return nil, fmt.Errorf("fetch forge promotions failed: %w", err)
	}

	versions, err := parseSupportedMinecraftVersions(data)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf(
			"forge promotions is empty; see %s",
			docsURL,
		)
	}

	return versions, nil
}

func parseSupportedMinecraftVersions(data []byte) ([]string, error) {
	var payload struct {
		Promos json.RawMessage `json:"promos"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("parse forge promotions failed: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(payload.Promos))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("parse forge promotions failed: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("parse forge promotions failed: promos is not an object")
	}

	seen := map[string]struct{}{}
	versions := make([]string, 0)
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("parse forge promotions failed: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("parse forge promotions failed: invalid promos key")
		}

		if _, err := dec.Token(); err != nil {
			return nil, fmt.Errorf("parse forge promotions failed: %w", err)
		}

		base, ok := strings.CutSuffix(key, "-recommended")
		if !ok {
			base, ok = strings.CutSuffix(key, "-latest")
		}
		if !ok {
			continue
		}
		if _, exists := seen[base]; exists {
			continue
		}
		if !strings.HasPrefix(base, "1.") {
			continue
		}
		seen[base] = struct{}{}
		versions = append(versions, base)
	}

	return versions, nil
}

func fetchVersion(gameVersion types.BareVersion) (string, error) {
	body, err := cache.CachedGetBytes(
		promotionsURL,
		cache.BytesRequestOptions{Kind: cache.KindMetadata},
	)
	if err != nil {
		return "", fmt.Errorf("fetch forge promotions failed: %w", err)
	}

	var data promotions
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("parse forge promotions failed: %w", err)
	}
	if len(data.Promos) == 0 {
		return "", fmt.Errorf("forge promotions is empty; see %s", docsURL)
	}

	keyBase := gameVersion.String()
	if v := data.Promos[keyBase+"-recommended"]; v != "" {
		return v, nil
	}
	if v := data.Promos[keyBase+"-latest"]; v != "" {
		return v, nil
	}

	return "", fmt.Errorf(
		"no forge version found for minecraft %s in promotions data; see %s (Forge) and %s (NeoForge comparison)",
		gameVersion,
		docsURL,
		neoForgeComparisonURL,
	)
}

func installerURL(
	gameVersion types.BareVersion,
	forgeVersion string,
) string {
	combinedVersion := fmt.Sprintf("%s-%s", gameVersion.String(), forgeVersion)
	escaped := url.PathEscape(combinedVersion)
	return fmt.Sprintf(
		"%s/%s/forge-%s-installer.jar",
		mavenBaseURL,
		escaped,
		escaped,
	)
}
