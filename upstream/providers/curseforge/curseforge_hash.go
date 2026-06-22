package curseforge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/mclucy/lucy/internal/artifacthash"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/upstream"
)

// fingerprintRequest is the body for POST /v1/fingerprints/432.
type fingerprintRequest struct {
	Fingerprints []uint32 `json:"fingerprints"`
}

// fingerprintResponse wraps the /v1/fingerprints/432 response.
// Verified against https://docs.curseforge.com/rest-api/#get-fingerprints-matches
type fingerprintResponse struct {
	Data struct {
		ExactMatches []struct {
			Id   uint32 `json:"id"`
			File struct {
				ModId int32 `json:"modId"`
			} `json:"file"`
		} `json:"exactMatches"`
	} `json:"data"`
}

// SlugFromFilePath computes the CurseForge fingerprint of the file at path,
// queries POST /v1/fingerprints/432, and returns the project slug.
// Returns ("", ErrProjectNotFound) if the file is not found on CurseForge.
func SlugFromFilePath(filePath string) (slug string, err error) {
	return SlugFromFilePathWithHint(filePath, "")
}

// SlugFromFilePathWithHint is like SlugFromFilePath but accepts an optional
// urlHint slug. URL hint is never trusted on its own — fingerprint always wins.
func SlugFromFilePathWithHint(filePath, urlHint string) (
	slug string,
	err error,
) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("curseforge hash: %w", err)
	}
	fp := artifacthash.Bytes(data).MurmurHash()
	return slugFromFingerprint(fp)
}

func (p provider) NameByHash(artifact upstream.Hashable) (
	name upstream.RemotePackageName,
	hash string,
	err error,
) {
	fingerprint := artifact.MurmurHash()
	hash = strconv.FormatUint(uint64(fingerprint), 10)
	slug, err := slugFromFingerprint(fingerprint)
	if err != nil {
		return name, hash, err
	}

	name = upstream.RemotePackageName{
		RemoteName: slug,
		Source:     p.Id(),
	}
	return name, hash, nil
}

func slugFromFingerprint(fp uint32) (string, error) {
	body, _ := json.Marshal(fingerprintRequest{Fingerprints: []uint32{fp}})
	req, err := http.NewRequest(
		http.MethodPost,
		baseUrl+"/v1/fingerprints/432",
		bytes.NewReader(body),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", ApiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	logger.Debug("curseforge fingerprint lookup")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer fn.CloseReader(resp.Body, logger.Warn)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf(
			"curseforge: fingerprint lookup returned status %d",
			resp.StatusCode,
		)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var result fingerprintResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", ErrProjectNotFound
	}
	if len(result.Data.ExactMatches) == 0 {
		return "", ErrProjectNotFound
	}

	modId := result.Data.ExactMatches[0].File.ModId
	mod, err := getModById(modId)
	if err != nil {
		return "", err
	}
	return mod.Slug, nil
}
