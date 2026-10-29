package info

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
)

// infoProjectEntry projects upstream project metadata. Version is the exact
// version info genuinely resolved, or Any when the request stayed unpinned.
func infoProjectEntry(
	metadata catalog.Metadata,
	ref catalog.Ref,
	resolved version.Version,
) infoEntry {
	entry := newInfoEntry()
	entry.Title = infoTitle(metadata.Title, ref.Name.String())
	entry.Summary = infoCleanLine(metadata.Brief)
	entry.Ref = infoProjectRef(
		ref.Name.String(),
		ref.Provider,
		resolved,
	)
	entry.License = infoCleanLine(metadata.License)
	entry.Description, entry.DescriptionFormat = infoDescription(metadata)
	entry.Authors = infoAuthors(metadata.Authors)
	entry.Links = infoLinks(metadata)
	return entry
}

// infoArtifactEntry projects one archive descriptor. Upstream metadata is
// merged only when the hash match is attributable to this descriptor.
func infoArtifactEntry(
	local artifact.PackageInfo,
	upstreamRef *catalog.VersionedRef,
	match *upstream.Info,
) infoEntry {
	entry := newInfoEntry()

	metadata := local.Metadata
	if match != nil {
		metadata = infoMergeMetadata(metadata, match.Metadata)
	}
	fallbackName := local.Ref.Name.String()
	if upstreamRef != nil {
		fallbackName = infoFirstNonEmpty(
			upstreamRef.Name.String(),
			fallbackName,
		)
	}
	entry.Title = infoTitle(metadata.Title, fallbackName)
	entry.Summary = infoCleanLine(metadata.Brief)
	if upstreamRef != nil {
		entry.Ref = infoProjectRef(
			upstreamRef.Name.String(),
			upstreamRef.Provider,
			upstreamRef.Version,
		)
		entry.Upstream = &infoUpstream{
			Ref: infoProjectRef(
				upstreamRef.Name.String(),
				upstreamRef.Provider,
				version.Any,
			),
			Version: infoVersionText(upstreamRef.Version),
		}
	} else {
		entry.Ref = infoProjectRef(
			local.Ref.Name.String(),
			local.Ref.Provider,
			local.Ref.Version,
		)
	}
	entry.License = infoCleanLine(metadata.License)
	entry.Description, entry.DescriptionFormat = infoDescription(metadata)
	entry.Authors = infoAuthors(metadata.Authors)
	entry.Links = infoLinks(metadata)
	entry.Identity = infoArtifactIdentity(local)
	entry.Needs, entry.Optional, entry.Dependencies = infoArtifactDependencies(local.Dependencies)
	entry.DependenciesKnown = true
	return entry
}

func infoArtifactIdentity(local artifact.PackageInfo) *infoIdentity {
	ecosystem := ""
	if local.Ref.Ecosystem != catalog.EcosystemUnspecified {
		ecosystem = local.Ref.Ecosystem.String()
	}
	return &infoIdentity{
		Name:      infoCleanLine(local.Ref.Name.String()),
		Version:   infoVersionText(local.Ref.Version),
		Provider:  infoProviderText(local.Ref.Provider),
		Ecosystem: ecosystem,
	}
}

// infoArtifactUpstream reports the hash match worth applying to this
// descriptor. A single-descriptor archive is unambiguous even when the local
// module id differs from the upstream slug. A multi-descriptor archive needs
// the descriptor to name the matched project.
func infoArtifactUpstream(
	local artifact.PackageInfo,
	single bool,
	lookup artifactLookup,
) (*catalog.VersionedRef, *upstream.Info) {
	if lookup.Ref == nil || !lookup.Matched() {
		return nil, nil
	}
	if single {
		return lookup.Ref, lookup.Info
	}
	if infoNormalizedName(local.Ref.Name.String()) ==
		infoNormalizedName(lookup.Ref.Name.String()) {
		return lookup.Ref, lookup.Info
	}
	return nil, nil
}

// infoMergeMetadata lets upstream project content win while the archive fills
// the gaps. The description is one unit: never mix one document's text with
// another document's URL or format.
func infoMergeMetadata(
	local catalog.Metadata,
	remote catalog.Metadata,
) catalog.Metadata {
	merged := catalog.Metadata{
		Title:   infoFirstNonEmpty(remote.Title, local.Title),
		Brief:   infoFirstNonEmpty(remote.Brief, local.Brief),
		License: infoFirstNonEmpty(remote.License, local.License),
		Authors: infoFirstNonEmptySlice(remote.Authors, local.Authors),
		URLs:    infoFirstNonEmptySlice(remote.URLs, local.URLs),
	}
	if remote.Description != "" || remote.DescriptionURL != "" {
		merged.Description = remote.Description
		merged.DescriptionURL = remote.DescriptionURL
		merged.DescriptionIsMarkdown = remote.DescriptionIsMarkdown
	} else {
		merged.Description = local.Description
		merged.DescriptionURL = local.DescriptionURL
		merged.DescriptionIsMarkdown = local.DescriptionIsMarkdown
	}
	return merged
}

func infoDescription(metadata catalog.Metadata) (string, string) {
	format := infoFormatText
	if metadata.DescriptionIsMarkdown {
		format = infoFormatMarkdown
	}
	return infoCleanText(metadata.Description), format
}

func infoTitle(title, fallback string) string {
	if title = infoCleanLine(title); title != "" {
		return title
	}
	return infoHumanTitle(fallback)
}

func infoHumanTitle(name string) string {
	cleaned := infoCleanLine(
		strings.NewReplacer("-", " ", "_", " ").Replace(name),
	)
	if cleaned == "" {
		return ""
	}
	runes := []rune(cleaned)
	runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
	return string(runes)
}

func infoAuthors(persons []catalog.Person) []infoAuthor {
	authors := make([]infoAuthor, 0, len(persons))
	seen := make(map[string]struct{}, len(persons))
	for _, person := range persons {
		name := infoCleanLine(person.Name)
		if name == "" {
			continue
		}
		url := infoCleanLine(person.URL)
		key := name + "\x00" + url
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		authors = append(authors, infoAuthor{
			Name: name,
			Role: infoCleanLine(person.Role),
			URL:  url,
		})
	}
	return authors
}

var infoLinkRanks = map[string]int{
	infoLinkHomepage: 0,
	infoLinkSource:   1,
	infoLinkIssues:   2,
	infoLinkWiki:     3,
	infoLinkDownload: 4,
	infoLinkForum:    5,
	infoLinkFunding:  6,
	infoLinkOther:    7,
}

func infoLinks(metadata catalog.Metadata) []infoLink {
	links := make([]infoLink, 0, len(metadata.URLs)+1)
	for _, url := range metadata.URLs {
		raw := infoCleanLine(url.URL)
		if raw == "" {
			continue
		}
		kind := infoLinkKind(url.Type)
		label := infoCleanLine(url.Name)
		if label == "" {
			label = infoLinkLabel(kind)
		}
		links = append(links, infoLink{
			Label: label,
			Kind:  kind,
			URL:   raw,
		})
	}
	if strings.TrimSpace(metadata.Description) == "" {
		if raw := infoCleanLine(metadata.DescriptionURL); raw != "" {
			links = append(links, infoLink{
				Label: "Full description",
				Kind:  infoLinkOther,
				URL:   raw,
			})
		}
	}
	sort.SliceStable(links, func(i, j int) bool {
		left := infoLinkRanks[links[i].Kind]
		right := infoLinkRanks[links[j].Kind]
		return left < right
	})

	deduplicated := make([]infoLink, 0, len(links))
	seen := make(map[string]struct{}, len(links))
	for _, link := range links {
		if _, exists := seen[link.URL]; exists {
			continue
		}
		seen[link.URL] = struct{}{}
		deduplicated = append(deduplicated, link)
	}
	return deduplicated
}

func infoLinkKind(urlType catalog.URLType) string {
	switch urlType {
	case catalog.URLHome:
		return infoLinkHomepage
	case catalog.URLSource:
		return infoLinkSource
	case catalog.URLIssues:
		return infoLinkIssues
	case catalog.URLWiki:
		return infoLinkWiki
	case catalog.URLFile:
		return infoLinkDownload
	case catalog.URLForum:
		return infoLinkForum
	case catalog.URLSponsor:
		return infoLinkFunding
	default:
		return infoLinkOther
	}
}

func infoLinkLabel(kind string) string {
	switch kind {
	case infoLinkHomepage:
		return "Homepage"
	case infoLinkSource:
		return "Source"
	case infoLinkIssues:
		return "Issues"
	case infoLinkWiki:
		return "Wiki"
	case infoLinkDownload:
		return "Download"
	case infoLinkForum:
		return "Forum"
	case infoLinkFunding:
		return "Funding"
	default:
		return "Link"
	}
}

func infoArtifactDependencies(
	dependencies []artifact.Dependency,
) ([]infoDependency, []infoDependency, []infoDependency) {
	all := make([]infoDependency, 0, len(dependencies))
	needs := make([]infoDependency, 0, len(dependencies))
	optional := make([]infoDependency, 0, len(dependencies))
	for _, dependency := range dependencies {
		ref := infoDependencyRef(dependency.Ref)
		if ref == "" {
			continue
		}
		constraint := dependency.Constraint
		relationship := ""
		switch dependency.Kind {
		case catalog.DependencyKindIncompatible, catalog.DependencyKindConflict:
			relationship = string(dependency.Kind)
			constraint = constraint.Negate()
		}
		projected := infoDependency{
			Ref:          ref,
			Constraint:   infoConstraintText(constraint),
			Distribution: infoDependencyDistribution(dependency.Origin),
			Required:     dependency.Mandatory,
			Relationship: relationship,
		}
		all = append(all, projected)
		if relationship != "" {
			continue
		}
		switch projected.Distribution {
		case infoDistributionBundled, infoDistributionServer:
			continue
		}
		if projected.Required {
			needs = append(needs, projected)
		} else {
			optional = append(optional, projected)
		}
	}
	return needs, optional, all
}

func infoDependencyDistribution(
	origin catalog.DependencyOrigin,
) string {
	switch catalog.NormalizeDependencyOrigin(origin) {
	case catalog.DependencyEmbedded:
		return infoDistributionBundled
	case catalog.DependencyServerProvided:
		return infoDistributionServer
	default:
		return infoDistributionSeparate
	}
}

// infoConstraintText renders a descriptor constraint as an actionable string.
// An unconstrained dependency keeps an empty constraint rather than claiming
// the literal selector "any".
func infoConstraintText(expr version.Expr) string {
	if len(expr) == 0 {
		return ""
	}

	alternatives := make([]string, 0, len(expr))
	for _, conjunction := range expr {
		terms := make([]string, 0, len(conjunction))
		for _, term := range conjunction {
			if term.Operator == version.OpNever {
				terms = append(terms, "never")
				continue
			}
			value, ok := term.Value.(fmt.Stringer)
			if !ok || value.String() == "" {
				continue
			}
			terms = append(terms, term.Operator.ToSign()+value.String())
		}
		if len(terms) > 0 {
			alternatives = append(alternatives, strings.Join(terms, " and "))
		}
	}
	return infoCleanLine(strings.Join(alternatives, " or "))
}

func infoDependencyRef(ref catalog.VersionedRef) string {
	name := infoCleanLine(ref.Name.String())
	if name == "" {
		return ""
	}
	if infoProviderUsable(ref.Provider) {
		return ref.Provider.String() + ":" + name
	}
	return name
}

func infoProjectRef(
	name string,
	provider catalog.Provider,
	projectVersion version.Version,
) string {
	name = infoCleanLine(name)
	if name == "" {
		return ""
	}
	ref := name
	if infoProviderUsable(provider) {
		ref = provider.String() + ":" + name
	}
	if resolved := infoVersionText(projectVersion); resolved != "" {
		ref += "@" + resolved
	}
	return ref
}

// infoVersionText reports a concrete version. Selectors and invalid sentinels
// are not facts about a package and stay out of the document.
func infoVersionText(value version.Version) string {
	if value == "" || value.CanInfer() || value.IsInvalid() {
		return ""
	}
	return infoCleanLine(value.String())
}

func infoProviderUsable(provider catalog.Provider) bool {
	return provider != catalog.ProviderUnspecified &&
		provider != catalog.ProviderUnknown
}

func infoProviderText(provider catalog.Provider) string {
	if !infoProviderUsable(provider) {
		return ""
	}
	return provider.String()
}

func infoNormalizedName(name string) string {
	return input.ToProjectName(name).String()
}

func infoFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func infoFirstNonEmptySlice[T any](slices ...[]T) []T {
	for _, slice := range slices {
		if len(slice) > 0 {
			return slice
		}
	}
	return nil
}

// infoCleanLine removes upstream terminal controls and collapses a value that
// belongs on one line.
func infoCleanLine(value string) string {
	value = ansi.Strip(value)
	var clean strings.Builder
	clean.Grow(len(value))
	for _, character := range value {
		switch {
		case character == '\n' || character == '\r' || character == '\t':
			clean.WriteByte(' ')
		case character < 0x20 || (character >= 0x7f && character <= 0x9f):
			continue
		default:
			clean.WriteRune(character)
		}
	}
	return strings.Join(strings.Fields(clean.String()), " ")
}

// infoCleanText keeps the structure of prose while removing terminal
// controls. The document carries the full description; the renderer bounds
// its own view.
func infoCleanText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = ansi.Strip(value)
	var clean strings.Builder
	clean.Grow(len(value))
	for _, character := range value {
		switch {
		case character == '\n' || character == '\t':
			clean.WriteRune(character)
		case character < 0x20 || (character >= 0x7f && character <= 0x9f):
			continue
		default:
			clean.WriteRune(character)
		}
	}
	return strings.TrimSpace(clean.String())
}

// infoCleanPath removes terminal controls from a local path without changing
// its meaningful whitespace.
func infoCleanPath(value string) string {
	value = ansi.Strip(value)
	var clean strings.Builder
	clean.Grow(len(value))
	for _, character := range value {
		switch {
		case character == '\n' || character == '\r' || character == '\t':
			clean.WriteByte(' ')
		case character < 0x20 || (character >= 0x7f && character <= 0x9f):
			continue
		default:
			clean.WriteRune(character)
		}
	}
	return strings.TrimSpace(clean.String())
}
