package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"strings"

	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/util"
)

var (
	forgeDocsURL       = "https://files.minecraftforge.net/"
	forgePromotionsURL = "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json"
	forgeMavenBaseURL  = "https://maven.minecraftforge.net/net/minecraftforge/forge"

	// Forge/NeoForge installation differences (official docs):
	// 1) Artifact naming:
	//    Forge: forge-{mc_version}-{forge_version}-installer.jar
	//    NeoForge: neoforge-{version}-installer.jar
	// 2) Version metadata source:
	//    Forge: promotions_slim.json on files.minecraftforge.net
	//    NeoForge: release index from maven.neoforged.net
	// 3) Installation command:
	//    Both use: java -jar <installer>.jar --installServer
	forgeNeoForgeDiffDocURL = "https://docs.neoforged.net/user/docs/server"
)

type forgePromotions struct {
	Promos map[string]string `json:"promos"`
}

func init() {
	registerInstaller(types.Forge, installForgeMod)
}

func installForgeMod(p types.Package) error {
	return installModLoaderPackage(p, types.Forge)
}

func installForge(p types.PackageId) error {
	panic("Forge installation is not implemented yet")

	fileURL := ""

	serverInfo := probe.ServerInfo()
	if serverInfo.WorkPath == "" {
		return errors.New("server working directory not found")
	}

	if fileURL == "" {
		if serverInfo.Executable == nil || serverInfo.Executable == probe.UnknownExecutable {
			return fmt.Errorf(
				"no executable found, cannot infer minecraft version for forge bootstrap; see %s",
				forgeDocsURL,
			)
		}
		if serverInfo.Executable.GameVersion == types.VersionUnknown {
			return fmt.Errorf(
				"unknown minecraft version, cannot infer forge bootstrap artifact; see %s",
				forgeDocsURL,
			)
		}

		forgeVersion, err := fetchForgeVersion(serverInfo.Executable.GameVersion)
		if err != nil {
			return err
		}
		fileURL = resolveForgeInstallerURL(
			serverInfo.Executable.GameVersion,
			forgeVersion,
		)
	}

	installer, _, err := util.DownloadFile(fileURL, serverInfo.WorkPath)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer func() { _ = installer.Close() }()

	if err := runForgeInstaller(
		installer.Name(),
		serverInfo.WorkPath,
	); err != nil {
		return err
	}

	return nil
}

func fetchForgeVersion(gameVersion types.RawVersion) (string, error) {
	res, err := http.Get(forgePromotionsURL)
	if err != nil {
		return "", fmt.Errorf("fetch forge promotions failed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf(
			"fetch forge promotions failed: status %d",
			res.StatusCode,
		)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("read forge promotions failed: %w", err)
	}

	var data forgePromotions
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("parse forge promotions failed: %w", err)
	}
	if len(data.Promos) == 0 {
		return "", fmt.Errorf("forge promotions is empty; see %s", forgeDocsURL)
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
		forgeDocsURL,
		forgeNeoForgeDiffDocURL,
	)
}

func resolveForgeInstallerURL(
	gameVersion types.RawVersion,
	forgeVersion string,
) string {
	combinedVersion := fmt.Sprintf("%s-%s", gameVersion.String(), forgeVersion)
	escaped := url.PathEscape(combinedVersion)
	return fmt.Sprintf(
		"%s/%s/forge-%s-installer.jar",
		forgeMavenBaseURL,
		escaped,
		escaped,
	)
}

func runForgeInstaller(installerPath string, workPath string) error {
	cmd := exec.Command("java", "-jar", installerPath, "--installServer")
	cmd.Dir = workPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		out := strings.TrimSpace(string(output))
		if out == "" {
			return fmt.Errorf("run forge installer failed: %w", err)
		}
		return fmt.Errorf("run forge installer failed: %w: %s", err, out)
	}
	return nil
}
