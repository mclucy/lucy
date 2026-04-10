package curseforge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/slugresolve"
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
func SlugFromFilePathWithHint(filePath, urlHint string) (slug string, err error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("curseforge hash: %w", err)
	}
	fp := curseForgeFingerprint(data)
	return slugFromFingerprint(fp)
}

func slugFromFingerprint(fp uint32) (string, error) {
	body, _ := json.Marshal(fingerprintRequest{Fingerprints: []uint32{fp}})
	req, err := http.NewRequest(http.MethodPost, baseUrl+"/v1/fingerprints/432", bytes.NewReader(body))
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
	defer tools.CloseReader(resp.Body, logger.Warn)

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

// curseForgeFingerprint computes the CurseForge custom MurmurHash2 fingerprint.
// Algorithm: strip whitespace bytes (0x09, 0x0A, 0x0D, 0x20), then apply
// a custom MurmurHash2-like mixing with multiplex=1540483477.
// Reference: https://github.com/meza/curseforge-fingerprint
func curseForgeFingerprint(data []byte) uint32 {
	const multiplex uint32 = 1540483477

	normalizedLen := uint32(0)
	for _, b := range data {
		if !isCFWhitespace(b) {
			normalizedLen++
		}
	}

	h := uint32(1) ^ normalizedLen
	var pending uint32
	var pendingBits uint32

	for _, b := range data {
		if isCFWhitespace(b) {
			continue
		}
		pending |= uint32(b) << pendingBits
		pendingBits += 8
		if pendingBits == 32 {
			k := pending * multiplex
			k = (k ^ k>>24) * multiplex
			h = h*multiplex ^ k
			pending = 0
			pendingBits = 0
		}
	}

	if pendingBits > 0 {
		h = (h ^ pending) * multiplex
	}

	h = (h ^ h>>13) * multiplex
	return h ^ h>>15
}

func isCFWhitespace(b byte) bool {
	return b == 0x09 || b == 0x0A || b == 0x0D || b == 0x20
}

func init() {
	slugresolve.RegisterHashLookup(types.SourceCurseForge, SlugFromFilePathWithHint)
}
