package info

import (
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/version"
	"github.com/spf13/cobra"
)

func isArtifactPath(path string) (bool, error) {
	if !artifact.SupportsPath(path) {
		return false, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return true, fmt.Errorf("stat artifact %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return true, fmt.Errorf("artifact %q is not a regular file", path)
	}
	return true, nil
}

func actionArtifactInfo(cmd *cobra.Command, filePath string) error {
	infos, err := artifact.ReadInfo(filePath)
	if err != nil {
		return fmt.Errorf("analyze artifact %q: %w", filePath, err)
	}
	if len(infos) == 0 {
		return fmt.Errorf("no package metadata found in %q", filePath)
	}

	lookup := lookupArtifactUpstream(filePath)

	long, _ := cmd.Flags().GetBool(cli.FlagLong)

	mode := cli.JSONMode(cmd)
	if mode != cli.JSONNone {
		views := make([]artifactInfoView, len(infos))
		for i, info := range infos {
			views[i] = artifactInfoViewFromInfo(info, lookup)
		}
		payload := any(views)
		if len(views) == 1 {
			payload = views[0]
		}
		compact := mode == cli.JSONCompact
		if err := style.WriteJSON(cmd.OutOrStdout(), payload, compact); err != nil {
			return err
		}
		return nil
	}

	for i, info := range infos {
		if i > 0 {
			fmt.Println()
		}
		fmt.Print(renderArtifactInfo(info, lookup, long))
	}
	return nil
}

// artifactLookup is the upstream result for one artifact file. Ref is the
// project that matched the file hash, nil when nothing matched. Info is that
// project's metadata, nil when the metadata lookup failed. Warnings holds the
// problems found during the lookup.
type artifactLookup struct {
	Ref      *catalog.VersionedRef
	Info     *upstream.Info
	Warnings []string
}

func lookupArtifactUpstream(filePath string) artifactLookup {
	result := artifactLookup{}
	lookup := routing.ResolveArtifactByHash(artifact.File{Path: filePath})
	for _, providerErr := range lookup.Errors {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("could not look up this artifact on %s: %v",
				providerErr.Provider.Title(), providerErr.Err))
	}
	if !lookup.Matched() {
		if lookup.Complete() {
			result.Warnings = append(result.Warnings,
				"no upstream project matched this artifact hash")
		} else {
			result.Warnings = append(result.Warnings,
				"could not determine whether this artifact exists upstream")
		}
		return result
	}
	ref := lookup.Ref
	result.Ref = &ref

	providers, err := routing.ResolveInfoProviders(ref.Ecosystem, ref.Provider)
	if err != nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("cannot retrieve upstream metadata for %s: %v",
				ref.StringBase(), err))
		return result
	}

	info, infoErrors, err := routing.GetInfoFirstSuccess(providers, ref.Ref)
	for _, providerErr := range infoErrors {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("could not retrieve metadata from %s: %v",
				providerErr.Provider.Title(), providerErr.Err))
	}
	if err != nil {
		if len(infoErrors) == 0 {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("cannot retrieve upstream metadata for %s: %v",
					ref.StringBase(), err))
		}
		return result
	}
	result.Info = &info
	return result
}

// matchedUpstream returns the upstream Info for info when the hash match
// belongs to it. An archive with several descriptors matches only the
// descriptor with the same name and ecosystem.
func matchedUpstream(info artifact.PackageInfo, lookup artifactLookup) *upstream.Info {
	if lookup.Info == nil || lookup.Ref == nil {
		return nil
	}
	if info.Ref.Name != lookup.Ref.Name || info.Ref.Provider != lookup.Ref.Provider {
		return nil
	}
	return lookup.Info
}

// displayMetadata merges archive and upstream metadata; upstream values win,
// archive values fill the gaps.
func displayMetadata(info artifact.PackageInfo, match *upstream.Info) catalog.Metadata {
	local := info.Metadata
	if local.Title == "" {
		local.Title = style.Capitalize(strings.ReplaceAll(info.Ref.Name.String(), "-", " "))
	}
	if match == nil {
		return local
	}

	remote := match.Metadata
	merged := catalog.Metadata{
		Title:   firstNonEmpty(remote.Title, local.Title),
		Brief:   firstNonEmpty(remote.Brief, local.Brief),
		License: firstNonEmpty(remote.License, local.License),
		Authors: firstNonEmptySlice(remote.Authors, local.Authors),
		URLs:    firstNonEmptySlice(remote.URLs, local.URLs),
	}
	// The description fields are one unit. Do not mix upstream text with
	// archive text or a URL of another document.
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptySlice[T comparable](slices ...[]T) []T {
	for _, slice := range slices {
		if len(slice) > 0 {
			return slice
		}
	}
	return nil
}

// artifactInfoView is the --json form of one archive descriptor. Metadata is
// what the archive declares; Upstream holds the matched upstream project.
type artifactInfoView struct {
	File         string                   `json:"file"`
	Package      string                   `json:"package"`
	Ecosystem    string                   `json:"ecosystem"`
	Version      string                   `json:"version,omitempty"`
	Dependencies []artifactDependencyView `json:"dependencies,omitempty"`
	Metadata     catalog.Metadata         `json:"metadata"`
	Upstream     *upstreamMatchView       `json:"upstream,omitempty"`
	Warnings     []string                 `json:"warnings,omitempty"`
}

type upstreamMatchView struct {
	Ref      string            `json:"ref"`
	Metadata *catalog.Metadata `json:"metadata,omitempty"`
}

type artifactDependencyView struct {
	Package    string                   `json:"package"`
	Constraint string                   `json:"constraint"`
	Mandatory  bool                     `json:"mandatory"`
	Origin     catalog.DependencyOrigin `json:"origin"`
}

func artifactInfoViewFromInfo(
	info artifact.PackageInfo,
	lookup artifactLookup,
) artifactInfoView {
	dependencies := make(
		[]artifactDependencyView,
		0,
		len(info.Dependencies),
	)
	for _, dependency := range info.Dependencies {
		dependencies = append(dependencies, artifactDependencyView{
			Package:    formatArtifactRef(dependency.Ref),
			Constraint: formatArtifactConstraint(dependency.Constraint),
			Mandatory:  dependency.Mandatory,
			Origin:     catalog.NormalizeDependencyOrigin(dependency.Origin),
		})
	}

	view := artifactInfoView{
		File:         info.FilePath,
		Package:      formatArtifactRef(info.Ref),
		Ecosystem:    info.Ref.Ecosystem.String(),
		Version:      info.Ref.Version.String(),
		Dependencies: dependencies,
		Metadata:     info.Metadata,
		Warnings:     lookup.Warnings,
	}
	if lookup.Ref != nil {
		matchView := &upstreamMatchView{
			Ref: lookup.Ref.StringBase(),
		}
		if match := matchedUpstream(info, lookup); match != nil {
			matchView.Metadata = &match.Metadata
		}
		view.Upstream = matchView
	}
	return view
}

func renderArtifactInfo(
	info artifact.PackageInfo,
	lookup artifactLookup,
	longOutput bool,
) string {
	match := matchedUpstream(info, lookup)
	metadata := displayMetadata(info, match)
	installID := "local:" + info.FilePath
	if match != nil {
		installID = match.Ref.StringBase()
	}

	var out strings.Builder
	out.WriteString(renderInfo(metadata, installID, longOutput))
	out.WriteString("\n")
	out.WriteString(renderArtifactDetails(info, lookup))
	if dependencies := renderArtifactDependencies(info.Dependencies); dependencies != "" {
		out.WriteString("\n\n")
		out.WriteString(dependencies)
	}
	if warnings := renderArtifactWarnings(lookup.Warnings); warnings != "" {
		out.WriteString("\n\n")
		out.WriteString(warnings)
	}
	out.WriteString("\n")
	return out.String()
}

func renderArtifactDetails(
	info artifact.PackageInfo,
	lookup artifactLookup,
) string {
	rows := [][]string{
		{"File", info.FilePath},
		{"Package", formatArtifactRef(info.Ref)},
		{"Ecosystem", info.Ref.Ecosystem.Title()},
		{"Version", info.Ref.Version.String()},
	}
	if lookup.Ref != nil {
		rows = append(
			rows,
			[]string{"File upstream", lookup.Ref.StringBase()},
		)
	}
	if info.Compatibility.FoliaSupported {
		rows = append(rows, []string{"Compatibility", "Folia supported"})
	}
	return renderArtifactTable("ARTIFACT", rows)
}

func renderArtifactWarnings(warnings []string) string {
	if len(warnings) == 0 {
		return ""
	}
	lines := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		lines = append(lines, style.Warning("  Warning: "+warning))
	}
	return strings.Join(lines, "\n")
}

func renderArtifactDependencies(dependencies []artifact.Dependency) string {
	if len(dependencies) == 0 {
		return ""
	}

	rows := make([][]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		role := "optional"
		if dependency.Mandatory {
			role = "required"
		}
		rows = append(rows, []string{
			formatArtifactRef(dependency.Ref),
			strings.Join([]string{
				role,
				string(catalog.NormalizeDependencyOrigin(dependency.Origin)),
				formatArtifactConstraint(dependency.Constraint),
			}, " · "),
		})
	}
	return renderArtifactTable("DEPENDENCIES", rows)
}

func renderArtifactTable(title string, rows [][]string) string {
	width := min(80, style.TermWidth())
	return renderInfoSectionHeader(title, len(rows), len(rows), width, false) +
		"\n\n" +
		indentInfoBlock(table.New().
			Border(lipgloss.HiddenBorder()).
			Rows(rows...).
			StyleFunc(func(_ int, col int) lipgloss.Style {
				if col == 0 {
					return lipgloss.NewStyle().Bold(true)
				}
				return lipgloss.NewStyle()
			}).
			Render())
}

func formatArtifactRef(ref catalog.VersionedRef) string {
	if ref.Ecosystem == catalog.EcosystemUnspecified {
		return ref.Name.String()
	}
	return ref.Ecosystem.String() + "/" + ref.Name.String()
}

func formatArtifactConstraint(expr version.Expr) string {
	if len(expr) == 0 {
		return "any"
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
	if len(alternatives) == 0 {
		return "any"
	}
	return strings.Join(alternatives, " or ")
}
