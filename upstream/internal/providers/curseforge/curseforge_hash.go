package curseforge

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
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
			ID   uint32 `json:"id"`
			File struct {
				ID    int32 `json:"id"`
				ModId int32 `json:"modId"`
			} `json:"file"`
		} `json:"exactMatches"`
	} `json:"data"`
}

// ReleaseByHash identifies the installed artifact by its MurmurHash
// fingerprint and returns the release it was published as, in one lookup.
// A fingerprint with no exact match is a miss, not an error.
func (p *Provider) ReleaseByHash(
	ctx context.Context,
	artifact upstream.ArtifactFingerprint,
) (
	upstream.Release,
	bool,
	error,
) {
	fingerprint, err := artifact.MurmurHash()
	if err != nil {
		return upstream.Release{}, false, err
	}
	match, err := p.fingerprintMatch(ctx, fingerprint)
	if err != nil {
		if errors.Is(err, ErrProjectNotFound) {
			return upstream.Release{}, false, nil
		}
		return upstream.Release{}, false, err
	}

	mod, err := p.getModById(ctx, match.ModId)
	if err != nil {
		return upstream.Release{}, false, err
	}
	file, err := p.getModFileById(ctx, match.ModId, match.FileId)
	if err != nil {
		return upstream.Release{}, false, err
	}

	gameVersions, loaders := splitGameVersions(file.GameVersions)
	release := upstream.Release{
		Ref: catalog.VersionedRef{
			Name:      catalog.Name(mod.Slug),
			Provider:  p.Provider(),
			Ecosystem: platformFromCurseForgeFile(mod, file),
			Version:   version.Version(file.DisplayName),
		},
		Candidate:         candidateFromCurseForgeFile(match.ModId, *file),
		MinecraftVersions: gameVersions,
		Loaders:           loaders,
	}
	return release, true, nil
}

type fingerprintMatchResult struct {
	ModId  int32
	FileId int32
}

func (p *Provider) fingerprintMatch(
	ctx context.Context,
	fp uint32,
) (fingerprintMatchResult, error) {
	var zero fingerprintMatchResult
	if p.apiKey == "" {
		return zero, ErrNoApiKey
	}
	body, _ := json.Marshal(fingerprintRequest{Fingerprints: []uint32{fp}})
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		baseUrl+"/v1/fingerprints/432",
		bytes.NewReader(body),
	)
	if err != nil {
		return zero, err
	}
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	p.logger.Debug("curseforge fingerprint lookup")
	//nolint:bodyclose // CloseReader closes the response and logs close errors.
	resp, err := p.network.Do(req)
	if err != nil {
		return zero, err
	}
	defer fn.CloseReader(resp.Body, p.logger.Warn)

	if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf(
			"curseforge: fingerprint lookup returned status %d",
			resp.StatusCode,
		)
	}

	const maxFingerprintResponseBytes = 4 * 1024 * 1024
	raw, err := fsutil.CopyBytes(resp.Body, maxFingerprintResponseBytes)
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
		FileId: m.File.ID,
	}, nil
}

func platformFromCurseForgeFile(
	mod *modResponse,
	file *fileResponse,
) catalog.Ecosystem {
	for _, idx := range mod.LatestFilesIndexes {
		if idx.FileId == file.ID && idx.ModLoader != nil {
			return platformFromCurseForgeModLoader(*idx.ModLoader)
		}
	}
	return catalog.EcosystemUnspecified
}

func platformFromCurseForgeModLoader(loader int32) catalog.Ecosystem {
	for ecosystem, loaderType := range curseForgeLoaderTypes {
		if int32(loaderType) == loader {
			return ecosystem
		}
	}
	return catalog.EcosystemUnspecified
}
