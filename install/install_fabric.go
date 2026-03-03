package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/util"
)

var fabricMetaBaseURL = "https://meta.fabricmc.net"

// Docs: https://fabricmc.net/use/server/
// Fabric install bootstraps from the server launch jar and resolves versions via Fabric Meta.

type fabricInstallerVersion struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

type fabricLoaderVersionEntry struct {
	Loader struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	} `json:"loader"`
}

func init() {
	registerInstaller(types.Fabric, installFabricMod)
}

func installFabric(p types.Package) error {
	fileURL := ""
	if p.Remote != nil {
		fileURL = p.Remote.FileUrl
	}

	serverInfo := probe.ServerInfo()
	if serverInfo.Executable == probe.UnknownExecutable {
		return errors.New("server working directory not found")
	}

	if fileURL == "" {
		if serverInfo.Executable == nil || serverInfo.Executable == probe.UnknownExecutable {
			return errors.New("no executable found, cannot infer minecraft version for fabric bootstrap")
		}
		if serverInfo.Executable.GameVersion == types.UnknownVersion {
			return errors.New("unknown minecraft version, cannot infer fabric bootstrap artifact")
		}

		var err error
		fileURL, err = resolveFabricServerLaunchJarURL(serverInfo.Executable.GameVersion)
		if err != nil {
			return err
		}
	}

	file, _, err := util.DownloadFile(fileURL, serverInfo.WorkPath)
	tools.CloseReader(file, nil)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	return nil
}

func installFabricMod(p types.Package) error {
	if p.Id.Name == "fabric-loader" {
		err := installFabric(p)
		if err != nil {
			return err
		}
	}
	return installModLoaderPackage(p, types.Fabric)
}

func resolveFabricServerLaunchJarURL(gameVersion types.RawVersion) (
	string,
	error,
) {
	loaderVersion, err := fetchFabricLoaderVersion(gameVersion)
	if err != nil {
		return "", err
	}
	installerVersion, err := fetchFabricInstallerVersion()
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"%s/v2/versions/loader/%s/%s/%s/server/jar",
		fabricMetaBaseURL,
		url.PathEscape(gameVersion.String()),
		url.PathEscape(loaderVersion),
		url.PathEscape(installerVersion),
	), nil
}

func fetchFabricInstallerVersion() (string, error) {
	res, err := http.Get(fabricMetaBaseURL + "/v2/versions/installer")
	if err != nil {
		return "", fmt.Errorf("fetch fabric installer versions failed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf(
			"fetch fabric installer versions failed: status %d",
			res.StatusCode,
		)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("read fabric installer versions failed: %w", err)
	}

	var versions []fabricInstallerVersion
	if err := json.Unmarshal(body, &versions); err != nil {
		return "", fmt.Errorf("parse fabric installer versions failed: %w", err)
	}
	if len(versions) == 0 {
		return "", errors.New("no fabric installer versions available")
	}

	for _, v := range versions {
		if v.Stable {
			return v.Version, nil
		}
	}
	return versions[0].Version, nil
}

func fetchFabricLoaderVersion(gameVersion types.RawVersion) (string, error) {
	endpoint := fmt.Sprintf(
		"%s/v2/versions/loader/%s",
		fabricMetaBaseURL,
		url.PathEscape(gameVersion.String()),
	)
	res, err := http.Get(endpoint)
	if err != nil {
		return "", fmt.Errorf("fetch fabric loader versions failed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf(
			"fetch fabric loader versions failed: status %d",
			res.StatusCode,
		)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("read fabric loader versions failed: %w", err)
	}

	var versions []fabricLoaderVersionEntry
	if err := json.Unmarshal(body, &versions); err != nil {
		return "", fmt.Errorf("parse fabric loader versions failed: %w", err)
	}
	if len(versions) == 0 {
		return "", fmt.Errorf(
			"no fabric loader versions for game %s",
			gameVersion,
		)
	}

	for _, v := range versions {
		if v.Loader.Stable && v.Loader.Version != "" {
			return v.Loader.Version, nil
		}
	}
	if versions[0].Loader.Version == "" {
		return "", fmt.Errorf(
			"no usable fabric loader version for game %s",
			gameVersion,
		)
	}

	return versions[0].Loader.Version, nil
}
