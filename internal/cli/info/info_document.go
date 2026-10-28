package info

import (
	"time"

	"github.com/mclucy/lucy/internal/cli"
)

const (
	infoDocumentSchema = 1

	infoKindProject  = "project"
	infoKindArtifact = "artifact"

	infoFormatMarkdown = "markdown"
	infoFormatText     = "text"

	infoDistributionBundled  = "bundled"
	infoDistributionSeparate = "separate"
	infoDistributionServer   = "server"

	infoLinkHomepage = "homepage"
	infoLinkWiki     = "wiki"
	infoLinkIssues   = "issues"
	infoLinkSource   = "source"
	infoLinkDownload = "download"
	infoLinkForum    = "forum"
	infoLinkFunding  = "funding"
	infoLinkOther    = "other"

	infoErrorReference     = "info.invalid_reference"
	infoErrorAmbiguous     = "info.ambiguous_reference"
	infoErrorProvider      = "info.provider_failed"
	infoErrorProviderPick  = "info.provider_unavailable"
	infoErrorNotFound      = "info.not_found"
	infoErrorWorkspace     = "info.workspace_failed"
	infoErrorWrite         = "info.write_failed"
	infoErrorArtifactMiss  = "info.artifact_missing"
	infoErrorArtifactRead  = "info.artifact_unreadable"
	infoErrorArtifactEmpty = "info.artifact_empty"
)

// infoDocument is the one wire format both project information and local
// archive inspection produce. Entries are never folded here; the human
// renderer bounds its own view.
type infoDocument struct {
	Schema   int         `json:"schema"`
	Kind     string      `json:"kind"`
	File     string      `json:"file,omitempty"`
	Entries  []infoEntry `json:"entries"`
	Warnings []string    `json:"warnings"`
}

var infoJSON cli.JSONResult[infoDocument]

type infoEntry struct {
	Title             string           `json:"title"`
	Summary           string           `json:"summary"`
	Ref               string           `json:"ref"`
	Installed         *infoInstalled   `json:"installed,omitempty"`
	Needs             []infoDependency `json:"needs"`
	Optional          []infoDependency `json:"optional"`
	Links             []infoLink       `json:"links"`
	Description       string           `json:"description"`
	DescriptionFormat string           `json:"description_format"`
	Authors           []infoAuthor     `json:"authors"`
	License           string           `json:"license"`
	Identity          *infoIdentity    `json:"identity,omitempty"`
	Upstream          *infoUpstream    `json:"upstream,omitempty"`
	Dependencies      []infoDependency `json:"dependencies"`
	DependenciesKnown bool             `json:"dependencies_known"`

	// Builds preserves every build the source reported, in source order.
	Builds []infoBuild `json:"builds"`

	// buildCriteria is presentation context, never document data.
	buildCriteria *infoBuildCriteria
}

// infoBuild mirrors one published project version as reported upstream.
// Empty metadata means the source did not report it.
type infoBuild struct {
	Version      string    `json:"version"`
	GameVersions []string  `json:"game_versions"`
	Loaders      []string  `json:"loaders"`
	Channel      string    `json:"channel"`
	Published    time.Time `json:"published,omitzero"`
}

type infoInstalled struct {
	Ref     string `json:"ref"`
	Version string `json:"version"`
	Path    string `json:"path"`
}

type infoDependency struct {
	Ref          string `json:"ref"`
	Constraint   string `json:"constraint,omitempty"`
	Distribution string `json:"distribution"`
	Required     bool   `json:"required"`
	Relationship string `json:"relationship,omitempty"`
}

type infoLink struct {
	Label string `json:"label"`
	Kind  string `json:"kind"`
	URL   string `json:"url"`
}

type infoAuthor struct {
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
	URL  string `json:"url,omitempty"`
}

type infoIdentity struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Provider  string `json:"provider"`
	Ecosystem string `json:"ecosystem"`
}

type infoUpstream struct {
	Ref     string `json:"ref"`
	Version string `json:"version"`
}

func newInfoDocument(kind, file string) infoDocument {
	return infoDocument{
		Schema:   infoDocumentSchema,
		Kind:     kind,
		File:     file,
		Entries:  []infoEntry{},
		Warnings: []string{},
	}
}

func newInfoEntry() infoEntry {
	return infoEntry{
		Needs:        []infoDependency{},
		Optional:     []infoDependency{},
		Links:        []infoLink{},
		Authors:      []infoAuthor{},
		Dependencies: []infoDependency{},
		Builds:       []infoBuild{},
	}
}
