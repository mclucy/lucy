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
	"github.com/mclucy/lucy/upstream/internal/providers/retry"
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

	var data []byte
	outcome := retry.Do(
		ctx,
		retry.Default,
		func(ctx context.Context) retry.Attempt {
			res, err := p.requestBytes(ctx, u)
			if err != nil {
				return retry.Attempt{Err: err}
			}
			if res.StatusCode == 200 {
				data = res.Data
			}
			return retry.Attempt{
				Status:     res.StatusCode,
				RetryAfter: retry.RetryAfter(res.Header),
			}
		},
	)
	if outcome.Err != nil {
		return upstream.Release{}, false, outcome.Err
	}
	if outcome.Status == 404 {
		return upstream.Release{}, false, nil
	}
	if outcome.Status != 200 {
		return upstream.Release{}, false, fmt.Errorf(
			"modrinth: hash lookup returned status %d",
			outcome.Status,
		)
	}

	var ver versionResponse
	if err := json.Unmarshal(data, &ver); err != nil {
		return upstream.Release{}, false, err
	}

	// Validate the match before asking for the project, so an entry whose
	// files cannot identify the installed artifact costs one request.
	file, err := requireVersionFile(&ver, hash)
	if err != nil {
		return upstream.Release{}, false, err
	}

	project, err := p.getProjectById(ctx, ver.ProjectId)
	if err != nil {
		return upstream.Release{}, false, err
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
// API omits is a miss. Transient failures are retried with backoff and can
// split into smaller batches. A returned entry without a project or a file
// carrying the requested digest is an incomplete lookup, not a miss.
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
		found, batchErrors := p.versionFilesBatch(ctx, batch, 0)
		for hash, err := range batchErrors {
			versionErrors[hash] = err
		}
		maps.Copy(versions, found)
	}

	matchedFiles := make(map[string]fileResponse, len(versions))
	invalid := make(map[string]error, len(versions))
	for hash := range versions {
		ver := versions[hash]
		file, err := requireVersionFile(&ver, hash)
		if err != nil {
			invalid[hash] = err
			continue
		}
		matchedFiles[hash] = file
	}

	// Only entries that identify their artifact join the project and
	// dependency fan-outs.
	matched := make([]*versionResponse, 0, len(matchedFiles))
	projectIds := make([]string, 0, len(matchedFiles))
	for hash := range matchedFiles {
		ver := versions[hash]
		matched = append(matched, &ver)
		projectIds = append(projectIds, ver.ProjectId)
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
			if err := versionErrors[hash]; err != nil {
				lookups[i].Err = fmt.Errorf(
					"modrinth: hash lookup for %s: %w",
					hash,
					err,
				)
			}
			continue
		}
		if err := invalid[hash]; err != nil {
			lookups[i].Err = err
			continue
		}
		project := projects[ver.ProjectId]
		if project == nil {
			lookups[i].Err = projectErrors[ver.ProjectId]
			if lookups[i].Err == nil {
				lookups[i].Err = fmt.Errorf("%w: %s", ErrNoProject, ver.ProjectId)
			}
			lookups[i].Err = fmt.Errorf(
				"modrinth: project lookup for %s (hash %s): %w",
				ver.ProjectId,
				hash,
				lookups[i].Err,
			)
			continue
		}
		file := matchedFiles[hash]
		candidate, err := p.candidateFromVersion(ctx, &ver, file, true, true, dependencies)
		if err != nil {
			lookups[i].Err = fmt.Errorf(
				"modrinth: release for hash %s: %w",
				hash,
				err,
			)
			continue
		}
		lookups[i].Release = p.releaseFromVersion(project, &ver, candidate)
		lookups[i].Matched = true
	}
	return lookups
}

// requireVersionFile returns the file whose sha1 matches the requested digest.
// A record without a project or without that exact file cannot identify the
// installed artifact, so it is an invalid response for the lookup, not a
// miss.
func requireVersionFile(
	version *versionResponse,
	hash string,
) (fileResponse, error) {
	if version.ProjectId == "" {
		return fileResponse{}, fmt.Errorf(
			"%w: version %s for hash %s has no project",
			ErrInvalidAPIResponse,
			version.ID,
			hash,
		)
	}
	file, ok := fileBySha1(version.Files, hash)
	if !ok {
		return fileResponse{}, fmt.Errorf(
			"%w: version %s for hash %s has no file with that sha1",
			ErrInvalidAPIResponse,
			version.ID,
			hash,
		)
	}
	return file, nil
}

// versionFilesBatch resolves one batch of hashes. A batch that still fails
// after its retry budget is split and retried per part while the failure is
// transient, so one throttled response cannot leave every hash unresolved.
// Errors stay attached to the hashes of the part that carried them.
func (p *Provider) versionFilesBatch(
	ctx context.Context,
	hashes []string,
	depth int,
) (map[string]versionResponse, map[string]error) {
	found, err := p.versionFilesByHash(ctx, hashes)
	if err == nil {
		return found, nil
	}
	if len(hashes) > 1 &&
		depth < retry.MaxSplitDepth &&
		ctx.Err() == nil &&
		retry.Splittable(err) {
		middle := len(hashes) / 2
		leftFound, leftErrors := p.versionFilesBatch(ctx, hashes[:middle], depth+1)
		rightFound, rightErrors := p.versionFilesBatch(ctx, hashes[middle:], depth+1)
		mergedFound := make(
			map[string]versionResponse,
			len(leftFound)+len(rightFound),
		)
		maps.Copy(mergedFound, leftFound)
		maps.Copy(mergedFound, rightFound)
		mergedErrors := make(
			map[string]error,
			len(leftErrors)+len(rightErrors),
		)
		maps.Copy(mergedErrors, leftErrors)
		maps.Copy(mergedErrors, rightErrors)
		return mergedFound, mergedErrors
	}
	errorsByHash := make(map[string]error, len(hashes))
	for _, hash := range hashes {
		errorsByHash[hash] = err
	}
	return nil, errorsByHash
}

// fileBySha1 picks the file whose sha1 equals hash, case-insensitive. The
// matched file stands in for the artifact, so a version whose files carry no
// matching sha1 has no match: another file's bytes cannot stand in for the
// installed artifact.
func fileBySha1(files []fileResponse, hash string) (fileResponse, bool) {
	i := slices.IndexFunc(files, func(f fileResponse) bool {
		return strings.EqualFold(f.Hashes.Sha1, hash)
	})
	if i < 0 {
		return fileResponse{}, false
	}
	return files[i], true
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
