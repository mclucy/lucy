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

func showBatchPhase(header string, ids []types.PackageId) {
	logger.ShowInfo(fmt.Sprintf("==> %s: %s", header, joinPackageNames(ids)))
}

func showBatchSummary(installed int, failed int) {
	if failed == 0 {
		logger.ShowInfo(fmt.Sprintf("%d packages installed", installed))
	} else {
		logger.ShowInfo(fmt.Sprintf("%d installed, %d failed", installed, failed))
	}
}

func joinPackageNames(ids []types.PackageId) string {
	if len(ids) == 0 {
		return ""
	}
	if len(ids) == 1 {
		return ids[0].StringFull()
	}
	if len(ids) == 2 {
		return ids[0].StringFull() + " and " + ids[1].StringFull()
	}
	result := ""
	for i := 0; i < len(ids)-1; i++ {
		result += ids[i].StringFull() + ", "
	}
	result += "and " + ids[len(ids)-1].StringFull()
	return result
}
