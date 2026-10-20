package detector

import "github.com/mclucy/lucy/types"

type paperFamilyResult uint8

const (
	paperFamilyUnknown paperFamilyResult = iota
	familyStrong
	familyWeak
	familyMiss
	familyContradiction
)

type paperLineageResult uint8

const (
	paperLineageUnknown paperLineageResult = iota
	lineagePaper
	lineageFork
	lineageUnknown
	lineageContradiction
)

type paperObservations struct {
	hasPaperClasses              bool
	hasSpigotClasses             bool
	officialDistribution         bool
	detectedLineage              string
	gameVersion                  types.Version
	metaMainClass                string
	librariesListEntries         []string
	versionsListEntries          []string
	patchesListEntries           []string
	downloadContext              string
	versionJSONID                types.Version
	patchProperties              map[string]string
	hasPaperMCPatch              bool
	buildInfo                    string
	leavesclipVersion            string
	manifestMainClass            string
	manifestSpecificationTitle   string
	manifestSpecificationVendor  string
	manifestImplementationTitle  string
	manifestImplementationVendor string
	manifestImplementationVer    string
	hasLeaperNamespace           bool
	hasLeavesclipNamespace       bool
	hasPaperclipNamespace        bool
	hasLegacyPaperclipNamespace  bool
	hasYouerNamespace            bool
}

type paperJudgment struct {
	bukkitConfirmed    bool
	observations       paperObservations
	familyResult       paperFamilyResult
	lineageResult      paperLineageResult
	lineageName        string
	contradictionState string
	fastPathUsed       bool
	reasons            []string
}

func newPaperJudgment() paperJudgment {
	return paperJudgment{
		familyResult:  paperFamilyUnknown,
		lineageResult: paperLineageUnknown,
		reasons:       make([]string, 0, 8),
	}
}

func (j *paperJudgment) addReason(reason string) {
	if reason == "" {
		return
	}
	j.reasons = append(j.reasons, reason)
}
