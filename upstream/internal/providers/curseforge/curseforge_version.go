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
