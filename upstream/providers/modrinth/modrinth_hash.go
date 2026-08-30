package modrinth

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

// https://docs.modrinth.com/api/operations/versionfromhash/

const versionFileUrlPrefix = "https://api.modrinth.com/v2/version_file/"

// versionFileResponse is the response from GET /v2/version_file/{hash}.
type versionFileResponse struct {
	ProjectId     string   `json:"project_id"`
	VersionNumber string   `json:"version_number"`
	Loaders       []string `json:"loaders"`
}

// SlugFromFilePathWithHint is like SlugFromFilePath but accepts an optional
// urlHint slug. The hint is verified against the project's version file hashes
// before falling back to the authoritative hash lookup path.
func SlugFromFilePathWithHint(filePath, urlHint string) (
	slug string,
	err error,
) {
	sha1hex, err := sha1File(filePath)
	if err != nil {
		return "", fmt.Errorf("modrinth hash: %w", err)
	}

	if urlHint != "" && verifySlugBySha1(urlHint, sha1hex) {
		return urlHint, nil
	}

	return SlugFromHash(sha1hex)
}

func verifySlugBySha1(hintSlug, sha1hex string) bool {
	u, err := url.JoinPath(projectUrlPrefix, hintSlug, "version")
	if err != nil {
		return false
	}
	u += "?include_changelog=false"

	log.Debug("modrinth hint verification: " + u)
	res, err := requestBytes(u)
	if err != nil {
		return false
	}

	if res.StatusCode != 200 {
		return false
	}

	var versions []versionResponse
	if err := json.Unmarshal(res.Data, &versions); err != nil {
		return false
	}

	for _, version := range versions {
		for _, file := range version.Files {
			if strings.EqualFold(file.Hashes.Sha1, sha1hex) {
				return true
			}
		}
	}

	return false
}

// SlugFromHash queries Modrinth for a project by SHA-1 hash using the
// single-file endpoint GET /v2/version_file/{hash}?algorithm=sha1.
func SlugFromHash(sha1hex string) (slug string, err error) {
	u := versionFileUrlPrefix + sha1hex + "?algorithm=sha1"

	log.Debug("modrinth hash lookup: " + u)
	res, err := requestBytes(u)
	if err != nil {
		return "", err
	}

	if res.StatusCode == 404 {
		return "", ErrNoProject
	}
	if res.StatusCode != 200 {
		return "", fmt.Errorf(
			"modrinth: hash lookup returned status %d",
			res.StatusCode,
		)
	}

	var version versionFileResponse
	if err := json.Unmarshal(
		res.Data,
		&version,
	); err != nil || version.ProjectId == "" {
		return "", ErrNoProject
	}

	project, err := getProjectById(version.ProjectId)
	if err != nil {
		return "", err
	}
	return project.Slug, nil
}

func sha1File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s provider) PackageByHash(hashable upstream.Hashable) (
	ref types.FullPackageRef,
	hash string,
	ok bool,
	err error,
) {
	hashBytes, err := hashable.Sha1()
	if err != nil {
		return ref, hash, false, err
	}
	hash = hex.EncodeToString(hashBytes[:])
	u := versionFileUrlPrefix + hash + "?algorithm=sha1"

	log.Debug("modrinth hash lookup: " + u)

	res, err := requestBytes(u)
	if err != nil {
		return ref, hash, false, err
	}

	if res.StatusCode == 404 {
		return ref, hash, false, nil
	}
	if res.StatusCode != 200 {
		return ref, hash, false, fmt.Errorf(
			"modrinth: hash lookup returned status %d",
			res.StatusCode,
		)
	}

	var version versionFileResponse
	err = json.Unmarshal(res.Data, &version)
	if err != nil || version.ProjectId == "" {
		return ref, hash, false, nil
	}

	project, err := getProjectById(version.ProjectId)
	if err != nil {
		return ref, hash, false, err
	}

	ref = types.FullPackageRef{
		Eco:     ecosystemFromModrinthLoaders(version.Loaders),
		Name:    types.BarePackageName(project.Slug),
		Version: types.BareVersion(version.VersionNumber),
		Scope:   s.Id(),
	}
	return ref, hash, true, nil
}

func ecosystemFromModrinthLoaders(loaders []string) types.Ecosystem {
	for _, loader := range loaders {
		p := types.Ecosystem(loader)
		if p.IsModding() || p == types.EcoBukkit {
			return p
		}
	}
	if len(loaders) > 0 {
		return types.Ecosystem(loaders[0])
	}
	return types.EcoUnspecified
}
