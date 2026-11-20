package modrinth

import (
	"context"
	"encoding/hex"
	"encoding/json/v2"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

// https://docs.modrinth.com/api/operations/versionfromhash/
// https://docs.modrinth.com/api/operations/versionsfromhashes/
// https://docs.modrinth.com/api/operations/getversions/
// https://docs.modrinth.com/api/operations/getprojects/

const versionFileURLPrefix = "https://api.modrinth.com/v2/version_file/"

var _ upstream.ArtifactBatchReleaseMapper = (*Provider)(nil)

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

	candidate, err := p.candidateFromVersion(ctx, &ver, file, true, true, nil)
	if err != nil {
		return upstream.Release{}, false, err
	}

	return p.releaseFromVersion(project, &ver, candidate), true, nil
}

// releaseFromVersion labels the release with the project's slug and side
// labels and the version's compatibility tags.
func (p *Provider) releaseFromVersion(
	project *projectResponse,
	ver *versionResponse,
	candidate upstream.Candidate,
) upstream.Release {
	return upstream.Release{
		Ref: catalog.VersionedRef{
			Name:      catalog.Name(project.Slug),
			Provider:  p.Provider(),
			Ecosystem: ecosystemFromModrinthLoaders(ver.Loaders),
			Version:   versionpkg.Version(ver.VersionNumber),
		},
		Candidate:         candidate,
		MinecraftVersions: ver.GameVersions,
		Loaders:           ver.Loaders,
		ServerSide:        upstream.ServerSide(project.ServerSide),
		ClientSide:        project.ClientSide,
	}
}

// ReleasesByHash resolves fingerprints in input order. SHA-1 digests are
// looked up in bounded batches through POST /v2/version_files, matched
// projects in one bounded GET /v2/projects fan-out, and their pinned
// dependency versions in one bounded GET /v2/versions fan-out. A digest the
// API omits is a miss with no error; a failed request records its error
// against every input whose digest that batch carried.
func (p *Provider) ReleasesByHash(
	ctx context.Context,
	artifacts []upstream.ArtifactFingerprint,
) []upstream.ArtifactReleaseLookup {
	lookups := make([]upstream.ArtifactReleaseLookup, len(artifacts))
	if len(artifacts) == 0 {
		return lookups
	}
	// One fingerprint keeps the original single-hash path, which is the
	// query the endpoint offers for it.
	if len(artifacts) == 1 {
		release, matched, err := p.ReleaseByHash(ctx, artifacts[0])
		lookups[0] = upstream.ArtifactReleaseLookup{
			Release: release,
			Matched: matched,
			Err:     err,
		}
		return lookups
	}
	if err := ctx.Err(); err != nil {
		for i := range lookups {
			lookups[i].Err = err
		}
		return lookups
	}

	hashes := make([]string, len(artifacts))
	resolved := make([]bool, len(artifacts))
	unique := make([]string, 0, len(artifacts))
	for i, artifact := range artifacts {
		digest, err := artifact.SHA1()
		if err != nil {
			lookups[i].Err = err
			continue
		}
		hashes[i] = hex.EncodeToString(digest[:])
		resolved[i] = true
		unique = append(unique, hashes[i])
	}

	versions := make(map[string]versionResponse, len(unique))
	versionErrors := make(map[string]error)
	for batch := range slices.Chunk(batchIds(unique), batchSize) {
		found, err := p.versionFilesByHash(ctx, batch)
		if err != nil {
			for _, hash := range batch {
				versionErrors[hash] = err
			}
			continue
		}
		maps.Copy(versions, found)
	}

	matched := make([]*versionResponse, 0, len(versions))
	projectIds := make([]string, 0, len(versions))
	for _, ver := range versions {
		matched = append(matched, &ver)
		if ver.ProjectId != "" {
			projectIds = append(projectIds, ver.ProjectId)
		}
	}
	projects, projectErrors := p.getProjectsByIds(ctx, projectIds)
	dependencies := p.pinnedVersionLookup(ctx, matched)

	for i := range artifacts {
		if !resolved[i] {
			continue
		}
		hash := hashes[i]
		ver, ok := versions[hash]
		if !ok {
			lookups[i].Err = versionErrors[hash]
			continue
		}
		if ver.ProjectId == "" {
			continue
		}
		project := projects[ver.ProjectId]
		if project == nil {
			lookups[i].Err = projectErrors[ver.ProjectId]
			if lookups[i].Err == nil {
				lookups[i].Err = fmt.Errorf("%w: %s", ErrNoProject, ver.ProjectId)
			}
			continue
		}
		file, ok := fileBySha1(ver.Files, hash)
		if !ok {
			continue
		}
		candidate, err := p.candidateFromVersion(ctx, &ver, file, true, true, dependencies)
		if err != nil {
			lookups[i].Err = err
			continue
		}
		lookups[i].Release = p.releaseFromVersion(project, &ver, candidate)
		lookups[i].Matched = true
	}
	return lookups
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
