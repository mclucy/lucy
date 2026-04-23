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

type paperBrandResult uint8

const (
	paperBrandUnknown paperBrandResult = iota
	brandPaper
	brandFork
	brandUnknown
	brandContradiction
)

type paperObservations struct {
	hasPaperClasses      bool
	hasSpigotClasses     bool
	officialDistribution bool
	detectedBrand        string
	gameVersion          types.RawVersion
	metaMainClass                string
	librariesListEntries         []string
	versionsListEntries          []string
	patchesListEntries           []string
	downloadContext              string
	versionJSONID                types.RawVersion
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
	hasYouerNamespace            bool
}

type paperJudgment struct {
	bukkitConfirmed    bool
	observations       paperObservations
	familyResult       paperFamilyResult
	brandResult        paperBrandResult
	brandName          string
	contradictionState bool
	fastPathUsed       bool
	reasons            []string
}

func newPaperJudgment() paperJudgment {
	return paperJudgment{
		familyResult: paperFamilyUnknown,
		brandResult:  paperBrandUnknown,
		reasons:      make([]string, 0, 8),
	}
}

func (j *paperJudgment) addReason(reason string) {
	if reason == "" {
		return
	}
	j.reasons = append(j.reasons, reason)
}
