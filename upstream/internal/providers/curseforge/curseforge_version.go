package curseforge

import (
	"context"
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
// Files the API does not know are omitted; a 404 response means the same, not
// a failed lookup.
// Docs: https://docs.curseforge.com/rest-api/#get-files
func (p *Provider) getModFilesByIds(
	ctx context.Context,
	fileIDs []int32,
) ([]fileResponse, error) {
	if len(fileIDs) == 0 {
		return nil, nil
	}
	payload := struct {
		FileIds []int32 `json:"fileIds"`
	}{FileIds: fileIDs}
	var resp filesDataResponse
	if err := p.post(ctx, baseUrl+"/v1/mods/files", payload, &resp); err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return resp.Data, nil
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
