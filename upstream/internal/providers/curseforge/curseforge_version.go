package curseforge

import (
	"context"
	"errors"
	"strconv"
)

const modFilesPageSize = 50

// listAllFiles pages through every file of a mod for the given compatibility
// filter. An empty gameVersion and zero loaderType list all files unfiltered.
func (p *Provider) listAllFiles(
	ctx context.Context,
	modID int32,
	gameVersion string,
	loaderType int,
) ([]fileResponse, error) {
	var files []fileResponse
	for offset := 0; ; offset += modFilesPageSize {
		u := modFilesUrl(modID, gameVersion, loaderType) +
			"&index=" + strconv.Itoa(offset)
		var page filesResponse
		if err := p.get(ctx, u, &page); err != nil {
			return nil, err
		}
		files = append(files, page.Data...)
		if len(page.Data) < modFilesPageSize {
			break
		}
		if page.Pagination.TotalCount > 0 &&
			int32(len(files)) >= page.Pagination.TotalCount {
			break
		}
	}
	return files, nil
}

type modFileDataResponse struct {
	Data fileResponse `json:"data"`
}

func (p *Provider) getModFileById(
	ctx context.Context,
	modId, fileId int32,
) (*fileResponse, error) {
	u := modFileURL(modId, fileId)
	var resp modFileDataResponse
	if err := p.get(ctx, u, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// getModFilesByIds returns many files in one POST /v1/mods/files request.
// An omitted file is left out of the result so the caller can report an error
// for that file. Responses that omit requested files are not cached.
// Docs: https://docs.curseforge.com/rest-api/#get-files
func (p *Provider) getModFilesByIds(
	ctx context.Context,
	fileIDs []int32,
) (map[int32]fileResponse, error) {
	if len(fileIDs) == 0 {
		return nil, nil
	}
	payload := struct {
		FileIds []int32 `json:"fileIds"`
	}{FileIds: fileIDs}
	var resp filesDataResponse
	var files map[int32]fileResponse
	err := p.readPost(
		ctx,
		baseUrl+"/v1/mods/files",
		payload,
		&resp,
		func(resp *filesDataResponse) error {
			files = make(map[int32]fileResponse, len(resp.Data))
			for _, file := range resp.Data {
				files[file.ID] = file
			}
			for _, fileID := range fileIDs {
				if _, ok := files[fileID]; !ok {
					return errPartialLookupResponse
				}
			}
			return nil
		},
	)
	if err != nil && !errors.Is(err, errPartialLookupResponse) {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return files, nil
}

// hasSHA1 reports whether the file record carries a SHA-1 digest.
func (f fileResponse) hasSHA1() bool {
	for _, h := range f.Hashes {
		if h.Algo == 1 && h.Digest != "" {
			return true
		}
	}
	return false
}

// completeForRelease reports whether an inline file record already carries the
// identity and release details a release conversion needs. An incomplete
// record is refetched through POST /v1/mods/files, which returns the
// authoritative file schema.
func (f fileResponse) completeForRelease() bool {
	return f.ID != 0 && f.ModId != 0 && f.DisplayName != "" &&
		f.FileName != "" && f.hasSHA1() && len(f.GameVersions) > 0
}
