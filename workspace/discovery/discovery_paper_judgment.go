package discovery

import "github.com/mclucy/lucy/version"

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
	gameVersion                  version.Version
	metaMainClass                string
	librariesListEntries         []string
	versionsListEntries          []string
	patchesListEntries           []string
	downloadContext              string
	versionJSONID                version.Version
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
	bukkitConfirmed bool
	observations    paperObservations
	familyResult    paperFamilyResult
	lineageResult   paperLineageResult
	lineageName     string
	lineages        []string
	contradicted    bool
}
