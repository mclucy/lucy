package modrinth

import (
	"context"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

// https://docs.modrinth.com/api/operations/versionfromhash/

const versionFileURLPrefix = "https://api.modrinth.com/v2/version_file/"

// ReleaseByHash identifies an installed artifact by its SHA-1 hash and
// returns the release it was published as, in one lookup. A hash Modrinth
// does not know is a miss, not an error.
func (p *Provider) ReleaseByHash(
	ctx context.Context,
	artifact upstream.ArtifactFingerprint,
) (
	upstream.Release,
	bool,
	error,
) {
	hashBytes, err := artifact.SHA1()
	if err != nil {
		return upstream.Release{}, false, err
	}
	hash := hex.EncodeToString(hashBytes[:])
	u := versionFileURLPrefix + hash + "?algorithm=sha1"

	p.logger.Debug("modrinth hash lookup: " + u)

	res, err := p.requestBytes(ctx, u)
	if err != nil {
		return upstream.Release{}, false, err
	}
	if res.StatusCode == 404 {
		return upstream.Release{}, false, nil
	}
	if res.StatusCode != 200 {
		return upstream.Release{}, false, fmt.Errorf(
			"modrinth: hash lookup returned status %d",
			res.StatusCode,
		)
	}

	var ver versionResponse
	if err := json.Unmarshal(res.Data, &ver); err != nil {
		return upstream.Release{}, false, err
	}
	if ver.ProjectId == "" {
		return upstream.Release{}, false, nil
	}

	project, err := p.getProjectById(ctx, ver.ProjectId)
	if err != nil {
		return upstream.Release{}, false, err
	}

	file, ok := fileBySha1(ver.Files, hash)
	if !ok {
		return upstream.Release{}, false, nil
	}

	candidate, err := p.candidateFromVersion(ctx, &ver, file, true)
	if err != nil {
		return upstream.Release{}, false, err
	}

	release := upstream.Release{
		Ref: catalog.VersionedRef{
			Name:      catalog.Name(project.Slug),
			Provider:  p.Provider(),
			Ecosystem: ecosystemFromModrinthLoaders(ver.Loaders),
			Version:   versionpkg.Version(ver.VersionNumber),
		},
		Candidate:         candidate,
		MinecraftVersions: ver.GameVersions,
		Loaders:           ver.Loaders,
	}
	return release, true, nil
}

// fileBySha1 picks the file whose sha1 equals hash, case-insensitive. The
// matched file stands in for the artifact, so a version whose files carry no
// matching sha1 falls back to its primary file, then to its first file. A
// version without files has no match.
func fileBySha1(files []fileResponse, hash string) (fileResponse, bool) {
	if len(files) == 0 {
		return fileResponse{}, false
	}
	if i := slices.IndexFunc(files, func(f fileResponse) bool {
		return strings.EqualFold(f.Hashes.Sha1, hash)
	}); i >= 0 {
		return files[i], true
	}
	if i := slices.IndexFunc(files, func(f fileResponse) bool {
		return f.IsPrimary
	}); i >= 0 {
		return files[i], true
	}
	return files[0], true
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
