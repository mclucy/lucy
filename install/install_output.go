package install

import (
	"fmt"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/types"
)

func showFetchStart(id types.PackageId) {
	logger.ShowInfo(
		fmt.Sprintf(
			"fetching package metadata for %s",
			id.StringFull(),
		),
	)
}

func showFetchSuccess(p types.Package) {
	if p.Remote == nil {
		return
	}
	logger.ShowInfo(
		fmt.Sprintf(
			"package metadata fetched from %s, resolved to %s",
			p.Remote.Source.String(),
			p.Id.StringFull(),
		),
	)
}

func showDownloadStart(url string) {
	logger.ShowInfo(fmt.Sprintf("downloading from %s", url))
}

func showInstallComplete(path string) {
	logger.ShowInfo(fmt.Sprintf("installed package to %s", path))
}
