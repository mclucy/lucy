package install

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
)

type logJournal struct{}

func (l logJournal) Record(event Event) {
	switch event.Kind {
	case EventBatchPhase:
		log.ShowInfo(
			fmt.Sprintf(
				"==> %s: %s",
				event.Header,
				joinPackageNames(event.IDs),
			),
		)
	case EventBatchSummary:
		if event.Failed == 0 {
			log.ShowInfo(fmt.Sprintf("%d packages installed", event.Count))
		} else {
			log.ShowInfo(
				fmt.Sprintf(
					"%d installed, %d failed",
					event.Count,
					event.Failed,
				),
			)
		}
	case EventResolveStart:
		log.ShowInfo(
			fmt.Sprintf(
				"resolving dependencies for %s",
				joinPackageNames(event.Roots),
			),
		)
	case EventDownloadStart:
		log.ShowInfo(fmt.Sprintf("downloading %d artifacts", event.Count))
	case EventVerifyStart:
		log.ShowInfo(fmt.Sprintf("verifying %d artifacts locally", event.Count))
	case EventReconcileStart:
		log.ShowInfo("reconciling advisory and verified graphs")
	case EventReconcileDiff:
		verbals := []string{}
		if len(event.Diff.Missing) > 0 {
			verbals = append(
				verbals,
				fmt.Sprintf("+%d missing", len(event.Diff.Missing)),
			)
		}
		if len(event.Diff.Extra) > 0 {
			verbals = append(
				verbals,
				fmt.Sprintf("-%d extra", len(event.Diff.Extra)),
			)
		}
		if len(event.Diff.Tightened) > 0 {
			verbals = append(
				verbals,
				fmt.Sprintf("~%d tightened", len(event.Diff.Tightened)),
			)
		}
		log.ShowInfo("reconcile: " + joinStrings(verbals))
	case EventApplyStart:
		log.ShowInfo(fmt.Sprintf("applying %d changes", event.Count))
	case EventConflict:
		if event.Err != nil {
			log.Error(event.Err)
		}
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

func joinStrings(strs []string) string {
	if len(strs) == 0 {
		return "none"
	}
	return strings.Join(strs, ", ")
}
