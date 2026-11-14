package curseforge

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/upstream"
)

var (
	ErrNotModpack      = errors.New("curseforge: project is not a modpack")
	ErrInvalidFileID   = errors.New("curseforge: version must be a numeric file ID")
	ErrNoServerPack    = errors.New("curseforge: file has no server pack")
	ErrFileUnavailable = errors.New("curseforge: file is not available for download")
)

// ResolveImportPack resolves a CurseForge modpack and an optional file
// selector to the server pack of that file. project accepts a numeric mod ID
// or a slug. version is a numeric file ID; an empty version selects the newest
// available release file, preferring the release channel.
func (p *Provider) ResolveImportPack(
	ctx context.Context,
	project, version string,
) (upstream.ImportPack, error) {
	if err := ctx.Err(); err != nil {
		return upstream.ImportPack{}, err
	}
	mod, err := p.resolveModpack(ctx, project)
	if err != nil {
		return upstream.ImportPack{}, err
	}
	selected, err := p.selectModpackFile(ctx, mod, version)
	if err != nil {
		return upstream.ImportPack{}, err
	}
	serverPack, err := p.serverPackFor(ctx, mod.ID, selected)
	if err != nil {
		return upstream.ImportPack{}, err
	}
	downloadLink, ok := downloadableURL(serverPack)
	if !ok {
		return upstream.ImportPack{}, fmt.Errorf(
			"%w: server pack %q of %s",
			ErrFileUnavailable,
			serverPack.FileName,
			mod.Slug,
		)
	}
	return upstream.ImportPack{
		Provider: catalog.ProviderCurseForge,
		Project:  mod.Slug,
		Name:     mod.Name,
		Version:  selected.DisplayName,
		Filename: serverPack.FileName,
		URL:      downloadLink,
		Hashes:   fileSHA1Hashes(serverPack.Hashes),
	}, nil
}

// resolveModpack resolves project to a mod in the modpack class. A numeric
// project is a mod ID; anything else is a slug.
func (p *Provider) resolveModpack(
	ctx context.Context,
	project string,
) (*modResponse, error) {
	var (
		mod *modResponse
		err error
	)
	if id, parseErr := strconv.ParseInt(project, 10, 32); parseErr == nil {
		mod, err = p.getModById(ctx, int32(id))
	} else {
		mod, err = p.resolveModpackSlug(ctx, catalog.Name(project))
	}
	if err != nil {
		return nil, err
	}
	if mod.ClassId == nil || *mod.ClassId != modpacksClassId {
		return nil, fmt.Errorf("%w: %s", ErrNotModpack, mod.Slug)
	}
	return mod, nil
}

// resolveModpackSlug looks up a modpack by slug within the modpack class. It
// mirrors resolveSlug, which searches the mod class only.
func (p *Provider) resolveModpackSlug(
	ctx context.Context,
	slug catalog.Name,
) (*modResponse, error) {
	var resp searchResponse
	if err := p.get(ctx, slugSearchUrlForClass(slug, modpacksClassId), &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, ErrProjectNotFound
	}
	if len(resp.Data) == 1 {
		return &resp.Data[0], nil
	}
	for i := range resp.Data {
		if input.ToProjectName(resp.Data[i].Slug) == slug {
			return &resp.Data[i], nil
		}
	}
	return nil, ErrAmbiguousSlug
}

// selectModpackFile returns the file an import is based on: the file named by
// version when set, otherwise the newest available release file.
func (p *Provider) selectModpackFile(
	ctx context.Context,
	mod *modResponse,
	version string,
) (*fileResponse, error) {
	if version != "" {
		id, err := strconv.ParseInt(version, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrInvalidFileID, version)
		}
		file, err := p.getModFileById(ctx, mod.ID, int32(id))
		if err != nil {
			return nil, err
		}
		if file.ModId != mod.ID {
			return nil, fmt.Errorf(
				"curseforge: file %d does not belong to %s",
				id,
				mod.Slug,
			)
		}
		return file, nil
	}
	files, err := p.listAllFiles(ctx, mod.ID, "", 0)
	if err != nil {
		return nil, err
	}
	file := newestReleaseFile(files)
	if file == nil {
		return nil, fmt.Errorf("%w: %s has no available files", ErrFileUnavailable, mod.Slug)
	}
	return file, nil
}

// newestReleaseFile returns the newest available file, preferring release
// channel files over beta and alpha ones. Newest means the latest FileDate,
// with the higher file ID breaking ties. It returns nil when no file is
// available.
func newestReleaseFile(files []fileResponse) *fileResponse {
	var best *fileResponse
	for i := range files {
		f := &files[i]
		if !f.IsAvailable {
			continue
		}
		if best == nil || fileRanksAbove(f, best) {
			best = f
		}
	}
	return best
}

func fileRanksAbove(a, b *fileResponse) bool {
	aRelease := curseForgeChannel(a.ChannelCode) == upstream.ReleaseChannelRelease
	bRelease := curseForgeChannel(b.ChannelCode) == upstream.ReleaseChannelRelease
	if aRelease != bRelease {
		return aRelease
	}
	aDate := parseCurseforgeDate(a.FileDate)
	bDate := parseCurseforgeDate(b.FileDate)
	if !aDate.Equal(bDate) {
		return aDate.After(bDate)
	}
	return a.ID > b.ID
}

// serverPackFor returns the server pack for file: the file itself when it is
// a server pack, otherwise the file named by its serverPackFileId.
func (p *Provider) serverPackFor(
	ctx context.Context,
	modID int32,
	file *fileResponse,
) (*fileResponse, error) {
	if file.IsServerPack != nil && *file.IsServerPack {
		return file, nil
	}
	if file.ServerPackFileId == nil {
		return nil, fmt.Errorf(
			"%w: file %q; choose a file ID whose release ships a server pack",
			ErrNoServerPack,
			file.DisplayName,
		)
	}
	return p.getModFileById(ctx, modID, *file.ServerPackFileId)
}

// downloadableURL returns the file's download URL when the file is available
// and publishes one.
func downloadableURL(f *fileResponse) (string, bool) {
	if !f.IsAvailable || f.DownloadUrl == nil || *f.DownloadUrl == "" {
		return "", false
	}
	return *f.DownloadUrl, true
}

// fileSHA1Hashes returns the file's SHA-1 digest under the "sha1" key.
func fileSHA1Hashes(hashes []fileHash) map[string]string {
	out := map[string]string{}
	for _, h := range hashes {
		if h.Algo == 1 && h.Digest != "" {
			out["sha1"] = h.Digest
		}
	}
	return out
}
