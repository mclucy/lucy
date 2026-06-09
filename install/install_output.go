package install

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/types"
)

func showDownloadStart(url string) {
	logger.ShowInfo(fmt.Sprintf("downloading from %s", url))
}

func showInstallComplete(path string) {
	logger.ShowInfo(fmt.Sprintf("installed package to %s", path))
}

func showBatchPhase(header string, ids []types.VersionedPackageRef) {
	logger.ShowInfo(fmt.Sprintf("==> %s: %s", header, joinPackageNames(ids)))
}

func showBatchSummary(installed int, failed int) {
	if failed == 0 {
		logger.ShowInfo(fmt.Sprintf("%d packages installed", installed))
	} else {
		logger.ShowInfo(
			fmt.Sprintf(
				"%d installed, %d failed",
				installed,
				failed,
			),
		)
	}
}

func joinPackageNames(ids []types.VersionedPackageRef) string {
	if len(ids) == 0 {
		return ""
	}
	if len(ids) == 1 {
		return ids[0].StringFull()
	}
	if len(ids) == 2 {
		return ids[0].StringFull() + " and " + ids[1].StringFull()
	}
	parts := make([]string, 0, len(ids))
	for i := 0; i < len(ids)-1; i++ {
		parts = append(parts, ids[i].StringFull())
	}
	return strings.Join(parts, ", ") + ", and " + ids[len(ids)-1].StringFull()
}

func showRecursiveResolveStart(roots []types.VersionedPackageRef) {
	logger.ShowInfo(
		fmt.Sprintf(
			"resolving dependencies for %s",
			joinPackageNames(roots),
		),
	)
}

func showRecursiveDownloadStart(count int) {
	logger.ShowInfo(fmt.Sprintf("downloading %d artifacts", count))
}

func showRecursiveVerifyStart(count int) {
	logger.ShowInfo(fmt.Sprintf("verifying %d artifacts locally", count))
}

func showRecursiveReconcileStart() {
	logger.ShowInfo("reconciling advisory and verified graphs")
}

func showRecursiveReconcileDiff(diff ReconcileDiff) {
	verbals := []string{}
	if len(diff.Missing) > 0 {
		verbals = append(verbals, fmt.Sprintf("+%d missing", len(diff.Missing)))
	}
	if len(diff.Extra) > 0 {
		verbals = append(verbals, fmt.Sprintf("-%d extra", len(diff.Extra)))
	}
	if len(diff.Tightened) > 0 {
		verbals = append(
			verbals,
			fmt.Sprintf("~%d tightened", len(diff.Tightened)),
		)
	}
	logger.ShowInfo("reconcile: " + joinStrings(verbals))
}

func showRecursiveApplyStart(count int) {
	logger.ShowInfo(fmt.Sprintf("applying %d changes", count))
}

func showRecursiveConflict(err error) {
	logger.ShowInfo(fmt.Sprintf("conflict:\n%s", err.Error()))
}

func joinStrings(strs []string) string {
	if len(strs) == 0 {
		return "none"
	}
	return strings.Join(strs, ", ")
}
