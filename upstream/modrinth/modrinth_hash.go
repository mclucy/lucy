package modrinth

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
)

const versionFileUrlPrefix = "https://api.modrinth.com/v2/version_file/"

// versionFileResponse is the response from GET /v2/version_file/{hash}.
type versionFileResponse struct {
	ProjectId string `json:"project_id"`
}

// SlugFromFilePath computes the SHA-1 of the file at path, queries
// Modrinth's single-file hash endpoint, and returns the project slug.
// Returns ("", ENoProject) if the file is not found on Modrinth.
func SlugFromFilePath(filePath string) (slug string, err error) {
	return SlugFromFilePathWithHint(filePath, "")
}

// SlugFromFilePathWithHint is like SlugFromFilePath but accepts an optional
// urlHint slug. The hint is verified against the project's version file hashes
// before falling back to the authoritative hash lookup path.
func SlugFromFilePathWithHint(filePath, urlHint string) (slug string, err error) {
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
	u, _ := url.JoinPath(projectUrlPrefix, hintSlug, "version")
	u += "?include_changelog=false"

	logger.Debug("modrinth hint verification: " + u)
	resp, err := http.Get(u)
	if err != nil {
		return false
	}
	defer tools.CloseReader(resp.Body, logger.Warn)

	if resp.StatusCode != http.StatusOK {
		return false
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	var versions []versionResponse
	if err := json.Unmarshal(data, &versions); err != nil {
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

	logger.Debug("modrinth hash lookup: " + u)
	resp, err := http.Get(u)
	if err != nil {
		return "", err
	}
	defer tools.CloseReader(resp.Body, logger.Warn)

	if resp.StatusCode == http.StatusNotFound {
		return "", ENoProject
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var version versionFileResponse
	if err := json.Unmarshal(data, &version); err != nil || version.ProjectId == "" {
		return "", ENoProject
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
