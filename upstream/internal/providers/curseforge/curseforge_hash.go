package curseforge

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/internal/providers/retry"
	"github.com/mclucy/lucy/version"
)

// fingerprintRequest is the body for POST /v1/fingerprints/432.
type fingerprintRequest struct {
	Fingerprints []uint32 `json:"fingerprints"`
}

// fingerprintMatch is one exact fingerprint match. The response's top-level
// ID is a project identity the official schema leaves unspecified, so matching
// correlates on the file's own fileFingerprint and takes project ownership
// from its modId.
type fingerprintMatch struct {
	ID   int32        `json:"id"`
	File fileResponse `json:"file"`
}

// fingerprintResponse wraps the /v1/fingerprints/432 response.
// Verified against https://docs.curseforge.com/rest-api/#get-fingerprints-matches
// Only exactMatches are decoded: partial and fuzzy matches do not describe the
// exact artifact on disk and must never stand in for one.
type fingerprintResponse struct {
	Data struct {
		ExactMatches []fingerprintMatch `json:"exactMatches"`
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
	lookups := p.ReleasesByHash(
		ctx,
		[]upstream.ArtifactFingerprint{artifact},
	)
	result := lookups[0]
	return result.Release, result.Matched, result.Err
}

// ReleasesByHash resolves every fingerprint into the release it was published
// as, returning one result per input in input order.
//
// Fingerprints are deduplicated and looked up in bounded batches, so identical
// inputs share one lookup and the number of requests does not grow with a
// project's file count. Only exact matches count. An unknown fingerprint is a
// miss, not an error. A transient batch failure is retried with backoff and
// then isolated by splitting the batch within a bounded split budget; a part
// that still fails is an error for every input it covered. An exact match
// whose file or project details cannot be completed is an incomplete lookup,
// reported as an error rather than a miss.
func (p *Provider) ReleasesByHash(
	ctx context.Context,
	artifacts []upstream.ArtifactFingerprint,
) []upstream.ArtifactReleaseLookup {
	results := make([]upstream.ArtifactReleaseLookup, len(artifacts))
	if len(artifacts) == 0 {
		return results
	}
	if err := ctx.Err(); err != nil {
		for i := range results {
			results[i].Err = err
		}
		return results
	}

	unique, indexByFingerprint := fingerprintIndex(artifacts, results)
	files := p.exactFiles(ctx, unique, indexByFingerprint, results)
	p.refetchIncompleteFiles(ctx, files, indexByFingerprint, results)
	p.fillReleases(ctx, files, indexByFingerprint, results)
	return results
}

// fingerprintIndex computes every artifact fingerprint and records computation
// failures on their results. It returns the distinct fingerprints in first-seen
// order and maps each fingerprint to the input positions that asked for it.
func fingerprintIndex(
	artifacts []upstream.ArtifactFingerprint,
	results []upstream.ArtifactReleaseLookup,
) ([]uint32, map[uint32][]int) {
	unique := make([]uint32, 0, len(artifacts))
	indexByFingerprint := make(map[uint32][]int, len(artifacts))
	for i, artifact := range artifacts {
		fingerprint, err := artifact.MurmurHash()
		if err != nil {
			results[i].Err = err
			continue
		}
		if _, ok := indexByFingerprint[fingerprint]; !ok {
			unique = append(unique, fingerprint)
		}
		indexByFingerprint[fingerprint] = append(
			indexByFingerprint[fingerprint],
			i,
		)
	}
	return unique, indexByFingerprint
}

// failFingerprints records one batch error for every input that asked for any
// of fingerprints.
func failFingerprints(
	results []upstream.ArtifactReleaseLookup,
	indexByFingerprint map[uint32][]int,
	fingerprints []uint32,
	err error,
) {
	for _, fingerprint := range fingerprints {
		failFingerprint(results, indexByFingerprint, fingerprint, err)
	}
}

// failFingerprint records one error for every input that asked for fingerprint,
// naming the fingerprint so a shared batch failure stays correlated per input.
func failFingerprint(
	results []upstream.ArtifactReleaseLookup,
	indexByFingerprint map[uint32][]int,
	fingerprint uint32,
	err error,
) {
	correlated := fmt.Errorf("fingerprint %d: %w", fingerprint, err)
	for _, i := range indexByFingerprint[fingerprint] {
		results[i].Err = correlated
	}
}

// failMatchedFile records err for every input whose exact match resolved to
// fingerprints and drops those matches so they cannot produce a release. A
// matched artifact whose details cannot be completed is an incomplete lookup,
// not a clean miss.
func failMatchedFile(
	results []upstream.ArtifactReleaseLookup,
	indexByFingerprint map[uint32][]int,
	files map[uint32]fileResponse,
	fingerprints []uint32,
	err error,
) {
	for _, fingerprint := range fingerprints {
		failFingerprint(results, indexByFingerprint, fingerprint, err)
		delete(files, fingerprint)
	}
}

// exactFiles resolves fingerprints through POST /v1/fingerprints/432 in
// bounded batches. A match is kept only when its wire fingerprint is in the
// uint32 fingerprint space, names a requested input, and the record carries
// file identity. A fingerprint the API omits stays a miss, unless the batch
// carries a match it cannot correlate: such a match proves the batch matched
// something, so any input it leaves undetermined is an incomplete lookup
// rather than a clean miss.
func (p *Provider) exactFiles(
	ctx context.Context,
	fingerprints []uint32,
	indexByFingerprint map[uint32][]int,
	results []upstream.ArtifactReleaseLookup,
) map[uint32]fileResponse {
	files := make(map[uint32]fileResponse, len(fingerprints))
	// A request body independent of discovery order lets a repeated lookup
	// reuse the cached response.
	slices.Sort(fingerprints)
	for batch := range slices.Chunk(fingerprints, lookupBatchSize) {
		p.resolveFingerprintBatch(ctx, batch, 0, indexByFingerprint, results, files)
	}
	return files
}

// resolveFingerprintBatch resolves one request's fingerprints into files. A
// batch that still fails after its retry budget is split in halves and retried
// per part, so one transient failure cannot mark every input unresolved.
// Permanent statuses, cancellation, and the split-depth budget end the split;
// only the inputs carried by a failed part report that error.
func (p *Provider) resolveFingerprintBatch(
	ctx context.Context,
	batch []uint32,
	depth int,
	indexByFingerprint map[uint32][]int,
	results []upstream.ArtifactReleaseLookup,
	files map[uint32]fileResponse,
) {
	matches, err := p.fingerprintMatches(ctx, batch)
	if err != nil {
		if len(batch) > 1 &&
			depth < retry.MaxSplitDepth &&
			ctx.Err() == nil &&
			retry.Splittable(err) {
			middle := len(batch) / 2
			p.resolveFingerprintBatch(
				ctx,
				batch[:middle],
				depth+1,
				indexByFingerprint,
				results,
				files,
			)
			p.resolveFingerprintBatch(
				ctx,
				batch[middle:],
				depth+1,
				indexByFingerprint,
				results,
				files,
			)
			return
		}
		failFingerprints(results, indexByFingerprint, batch, err)
		return
	}
	wanted := make(map[uint32]struct{}, len(batch))
	for _, fingerprint := range batch {
		wanted[fingerprint] = struct{}{}
	}
	uncorrelated := 0
	for _, match := range matches {
		if match.File.FileFingerprint == nil {
			if len(batch) == 1 {
				failFingerprint(
					results,
					indexByFingerprint,
					batch[0],
					fmt.Errorf(
						"curseforge: exact match for file %d has no fingerprint",
						match.File.ID,
					),
				)
				continue
			}
			uncorrelated++
			continue
		}
		wireFingerprint := *match.File.FileFingerprint
		if wireFingerprint < 0 || wireFingerprint > math.MaxUint32 {
			// Narrowing an invalid wire value could alias a requested fingerprint.
			uncorrelated++
			continue
		}
		fingerprint := uint32(wireFingerprint)
		if _, ok := wanted[fingerprint]; !ok {
			uncorrelated++
			continue
		}
		if match.File.ID == 0 || match.File.ModId == 0 {
			failFingerprint(
				results,
				indexByFingerprint,
				fingerprint,
				fmt.Errorf(
					"curseforge: exact fingerprint match for file %d is missing file identity",
					match.File.ID,
				),
			)
			continue
		}
		if _, seen := files[fingerprint]; seen {
			continue
		}
		files[fingerprint] = match.File
	}
	if uncorrelated == 0 {
		return
	}
	// The batch matched at least one file, but this record cannot be tied
	// to an input. Correlated matches stay matchable and inputs with a
	// more specific error keep it; the rest are undetermined, not missing.
	for _, fingerprint := range batch {
		if _, matched := files[fingerprint]; matched {
			continue
		}
		if indexes := indexByFingerprint[fingerprint]; len(indexes) > 0 &&
			results[indexes[0]].Err != nil {
			continue
		}
		failFingerprint(
			results,
			indexByFingerprint,
			fingerprint,
			fmt.Errorf(
				"curseforge: %d exact matches in a batch of %d fingerprints carry no file fingerprint, so fingerprint %d cannot be correlated",
				uncorrelated,
				len(batch),
				fingerprint,
			),
		)
	}
}

// refetchIncompleteFiles replaces inline match records that lack release
// details with authoritative records from POST /v1/mods/files, batched by file
// ID. Once a refetch is required, an omitted, incomplete or contradictory
// authoritative record is an incomplete lookup for that input, never a
// fallback to partial inline data and never a clean miss.
func (p *Provider) refetchIncompleteFiles(
	ctx context.Context,
	files map[uint32]fileResponse,
	indexByFingerprint map[uint32][]int,
	results []upstream.ArtifactReleaseLookup,
) {
	fileIDs := make([]int32, 0, len(files))
	fingerprintsByFileID := make(map[int32][]uint32, len(files))
	for fingerprint, file := range files {
		if file.completeForRelease() {
			continue
		}
		if _, ok := fingerprintsByFileID[file.ID]; !ok {
			fileIDs = append(fileIDs, file.ID)
		}
		fingerprintsByFileID[file.ID] = append(
			fingerprintsByFileID[file.ID],
			fingerprint,
		)
	}
	// The request order must not depend on map iteration order.
	slices.Sort(fileIDs)

	for batch := range slices.Chunk(fileIDs, lookupBatchSize) {
		p.refetchFileBatch(
			ctx,
			batch,
			0,
			fingerprintsByFileID,
			files,
			indexByFingerprint,
			results,
		)
	}
}

// refetchFileBatch replaces incomplete file records for one request batch. A
// failed batch is split and retried per part while its failure is transient
// and the split budget allows, so a permanent failure stays bounded and each
// input reports its own outcome.
func (p *Provider) refetchFileBatch(
	ctx context.Context,
	batch []int32,
	depth int,
	fingerprintsByFileID map[int32][]uint32,
	files map[uint32]fileResponse,
	indexByFingerprint map[uint32][]int,
	results []upstream.ArtifactReleaseLookup,
) {
	authoritativeFiles, err := p.getModFilesByIds(ctx, batch)
	if err != nil {
		if len(batch) > 1 &&
			depth < retry.MaxSplitDepth &&
			ctx.Err() == nil &&
			retry.Splittable(err) {
			middle := len(batch) / 2
			p.refetchFileBatch(
				ctx,
				batch[:middle],
				depth+1,
				fingerprintsByFileID,
				files,
				indexByFingerprint,
				results,
			)
			p.refetchFileBatch(
				ctx,
				batch[middle:],
				depth+1,
				fingerprintsByFileID,
				files,
				indexByFingerprint,
				results,
			)
			return
		}
		for _, fileID := range batch {
			failMatchedFile(
				results,
				indexByFingerprint,
				files,
				fingerprintsByFileID[fileID],
				err,
			)
		}
		return
	}

	authoritative := make(map[int32]fileResponse, len(authoritativeFiles))
	for _, file := range authoritativeFiles {
		authoritative[file.ID] = file
	}
	for _, fileID := range batch {
		fingerprints := fingerprintsByFileID[fileID]
		file, ok := authoritative[fileID]
		if !ok {
			failMatchedFile(
				results,
				indexByFingerprint,
				files,
				fingerprints,
				fmt.Errorf(
					"curseforge: exact file %d is missing from the mod files lookup",
					fileID,
				),
			)
			continue
		}
		if file.ModId == 0 {
			failMatchedFile(
				results,
				indexByFingerprint,
				files,
				fingerprints,
				fmt.Errorf(
					"curseforge: file %d lookup returned no project identity",
					fileID,
				),
			)
			continue
		}
		for _, fingerprint := range fingerprints {
			if file.FileFingerprint != nil &&
				*file.FileFingerprint != int64(fingerprint) {
				failFingerprint(
					results,
					indexByFingerprint,
					fingerprint,
					fmt.Errorf(
						"curseforge: file %d has fingerprint %d, exact match reported %d",
						fileID,
						*file.FileFingerprint,
						fingerprint,
					),
				)
				delete(files, fingerprint)
				continue
			}
			exactModID := files[fingerprint].ModId
			if file.ModId != exactModID {
				failFingerprint(
					results,
					indexByFingerprint,
					fingerprint,
					fmt.Errorf(
						"curseforge: file %d belongs to mod %d, exact match reported mod %d",
						fileID,
						file.ModId,
						exactModID,
					),
				)
				delete(files, fingerprint)
				continue
			}
			if !file.completeForRelease() {
				failFingerprint(
					results,
					indexByFingerprint,
					fingerprint,
					fmt.Errorf(
						"curseforge: file %d lookup returned incomplete release details",
						fileID,
					),
				)
				delete(files, fingerprint)
				continue
			}
			files[fingerprint] = file
		}
	}
}

// fillReleases resolves each matched file's project through POST /v1/mods and
// converts the pair into a release. An exact match whose project the API omits
// is an incomplete lookup: the release has no project identity to name.
func (p *Provider) fillReleases(
	ctx context.Context,
	files map[uint32]fileResponse,
	indexByFingerprint map[uint32][]int,
	results []upstream.ArtifactReleaseLookup,
) {
	modIDs := make([]int32, 0, len(files))
	seen := make(map[int32]struct{}, len(files))
	for _, file := range files {
		if _, ok := seen[file.ModId]; ok {
			continue
		}
		seen[file.ModId] = struct{}{}
		modIDs = append(modIDs, file.ModId)
	}
	// The request order must not depend on map iteration order.
	slices.Sort(modIDs)

	mods := make(map[int32]modResponse, len(modIDs))
	for batch := range slices.Chunk(modIDs, lookupBatchSize) {
		p.resolveModsBatch(ctx, batch, 0, mods, files, indexByFingerprint, results)
	}

	for fingerprint, file := range files {
		mod, ok := mods[file.ModId]
		if !ok {
			failFingerprint(
				results,
				indexByFingerprint,
				fingerprint,
				fmt.Errorf(
					"curseforge: project %d is missing from the mods lookup for exact file %d",
					file.ModId,
					file.ID,
				),
			)
			continue
		}
		release := p.releaseFromCurseForgeFile(&mod, &file)
		for _, i := range indexByFingerprint[fingerprint] {
			results[i] = upstream.ArtifactReleaseLookup{
				Release: release,
				Matched: true,
			}
		}
	}
}

// resolveModsBatch resolves one request's projects. A transient failure is
// split and retried per part while the split budget allows, so one throttled
// response does not discard every file in the batch; a permanent failure ends
// the split and reports only the inputs that part carried.
func (p *Provider) resolveModsBatch(
	ctx context.Context,
	batch []int32,
	depth int,
	mods map[int32]modResponse,
	files map[uint32]fileResponse,
	indexByFingerprint map[uint32][]int,
	results []upstream.ArtifactReleaseLookup,
) {
	found, err := p.getModsByIds(ctx, batch)
	if err != nil {
		if len(batch) > 1 &&
			depth < retry.MaxSplitDepth &&
			ctx.Err() == nil &&
			retry.Splittable(err) {
			middle := len(batch) / 2
			p.resolveModsBatch(
				ctx,
				batch[:middle],
				depth+1,
				mods,
				files,
				indexByFingerprint,
				results,
			)
			p.resolveModsBatch(
				ctx,
				batch[middle:],
				depth+1,
				mods,
				files,
				indexByFingerprint,
				results,
			)
			return
		}
		wanted := make(map[int32]struct{}, len(batch))
		for _, modID := range batch {
			wanted[modID] = struct{}{}
		}
		for fingerprint, file := range files {
			if _, ok := wanted[file.ModId]; !ok {
				continue
			}
			failFingerprint(results, indexByFingerprint, fingerprint, err)
			delete(files, fingerprint)
		}
		return
	}
	for _, mod := range found {
		mods[mod.ID] = mod
	}
}

// fingerprintMatches returns CurseForge's exact fingerprint matches for one
// batch. Partial matches are discarded; a 404 response means no matches.
// Docs: https://docs.curseforge.com/rest-api/#get-fingerprints-matches
func (p *Provider) fingerprintMatches(
	ctx context.Context,
	fingerprints []uint32,
) ([]fingerprintMatch, error) {
	if len(fingerprints) == 0 {
		return nil, nil
	}
	u := fmt.Sprintf("%s/v1/fingerprints/%d", baseUrl, minecraftGameId)
	var resp fingerprintResponse
	err := p.readPost(
		ctx,
		u,
		fingerprintRequest{Fingerprints: fingerprints},
		&resp,
	)
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return resp.Data.ExactMatches, nil
}

// releaseFromCurseForgeFile converts a project and one of its exact files into
// the release the file was published as.
func (p *Provider) releaseFromCurseForgeFile(
	mod *modResponse,
	file *fileResponse,
) upstream.Release {
	gameVersions, loaders := splitGameVersions(file.GameVersions)
	return upstream.Release{
		Ref: catalog.VersionedRef{
			Name:      catalog.Name(mod.Slug),
			Provider:  p.Provider(),
			Ecosystem: platformFromCurseForgeFile(mod, file),
			Version:   version.Version(file.DisplayName),
		},
		Candidate:         candidateFromCurseForgeFile(mod.ID, *file),
		MinecraftVersions: gameVersions,
		Loaders:           loaders,
	}
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
