package curseforge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/mclucy/lucy/internal/artifacthash"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
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

func (p provider) PackageByHash(artifact upstream.Hashable) (
	ref types.FullPackageRef,
	hash string,
	ok bool,
	err error,
) {
	fingerprint := artifact.MurmurHash()
	hash = strconv.FormatUint(uint64(fingerprint), 10)
	match, err := fingerprintMatch(fingerprint)
	if err != nil {
		if errors.Is(err, ErrProjectNotFound) {
			return ref, hash, false, nil
		}
		return ref, hash, false, err
	}

	mod, err := getModById(match.ModId)
	if err != nil {
		return ref, hash, false, err
	}
	file, err := getModFileById(match.ModId, match.FileId)
	if err != nil {
		return ref, hash, false, err
	}

	ref = types.FullPackageRef{
		PackageRef: types.PackageRef{
			Platform: platformFromCurseForgeFile(mod, file),
			Name:     types.BarePackageName(mod.Slug),
		},
		Version: types.BareVersion(file.DisplayName),
		Scope:   p.Id(),
	}
	return ref, hash, true, nil
}

type fingerprintMatchResult struct {
	ModId  int32
	FileId int32
}

func fingerprintMatch(fp uint32) (fingerprintMatchResult, error) {
	var zero fingerprintMatchResult
	body, _ := json.Marshal(fingerprintRequest{Fingerprints: []uint32{fp}})
	req, err := http.NewRequest(
		http.MethodPost,
		baseUrl+"/v1/fingerprints/432",
		bytes.NewReader(body),
	)
	if err != nil {
		return zero, err
	}
	req.Header.Set("x-api-key", ApiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	log.Debug("curseforge fingerprint lookup")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return zero, err
	}
	defer fn.CloseReader(resp.Body, log.Warn)

	if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf(
			"curseforge: fingerprint lookup returned status %d",
			resp.StatusCode,
		)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, err
	}

	var result fingerprintResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return zero, ErrProjectNotFound
	}
	if len(result.Data.ExactMatches) == 0 {
		return zero, ErrProjectNotFound
	}

	m := result.Data.ExactMatches[0]
	return fingerprintMatchResult{
		ModId:  m.File.ModId,
		FileId: int32(m.Id),
	}, nil
}

func slugFromFingerprint(fp uint32) (string, error) {
	match, err := fingerprintMatch(fp)
	if err != nil {
		return "", err
	}
	mod, err := getModById(match.ModId)
	if err != nil {
		return "", err
	}
	return mod.Slug, nil
}

func platformFromCurseForgeFile(mod *modResponse, file *fileResponse) types.PlatformId {
	for _, idx := range mod.LatestFilesIndexes {
		if idx.FileId == file.Id && idx.ModLoader != nil {
			return platformFromCurseForgeModLoader(*idx.ModLoader)
		}
	}
	return types.PlatformNone
}

func platformFromCurseForgeModLoader(loader int32) types.PlatformId {
	switch loader {
	case 1:
		return types.PlatformForge
	case 4:
		return types.PlatformFabric
	case 6:
		return types.PlatformNeoforge
	default:
		return types.PlatformNone
	}
}
