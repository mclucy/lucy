package modrinth

import (
	"context"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

// https://docs.modrinth.com/api/operations/versionfromhash/

const versionFileURLPrefix = "https://api.modrinth.com/v2/version_file/"

// versionFileResponse is the response from GET /v2/version_file/{hash}.
type versionFileResponse struct {
	ProjectId     string   `json:"project_id"`
	VersionNumber string   `json:"version_number"`
	Loaders       []string `json:"loaders"`
}

func (p *Provider) PackageByHash(
	ctx context.Context,
	hashable upstream.ArtifactFingerprint,
) (
	ref catalog.VersionedRef,
	hash string,
	ok bool,
	err error,
) {
	hashBytes, err := hashable.SHA1()
	if err != nil {
		return ref, hash, false, err
	}
	hash = hex.EncodeToString(hashBytes[:])
	u := versionFileURLPrefix + hash + "?algorithm=sha1"

	p.logger.Debug("modrinth hash lookup: " + u)

	res, err := p.requestBytes(ctx, u)
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

	project, err := p.getProjectById(ctx, version.ProjectId)
	if err != nil {
		return ref, hash, false, err
	}

	ref = catalog.VersionedRef{
		Name:      catalog.Name(project.Slug),
		Provider:  p.Provider(),
		Ecosystem: ecosystemFromModrinthLoaders(version.Loaders),
		Version:   versionpkg.Version(version.VersionNumber),
	}
	return ref, hash, true, nil
}

func ecosystemFromModrinthLoaders(loaders []string) catalog.Ecosystem {
	for _, loader := range loaders {
		p := catalog.Ecosystem(loader)
		if p.IsModding() || p == catalog.EcosystemBukkit || p == catalog.EcosystemPaper {
			return p
		}
	}
	if len(loaders) > 0 {
		return catalog.Ecosystem(loaders[0])
	}
	return catalog.EcosystemUnspecified
}
