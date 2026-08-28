package info

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/internal/artifacthash"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/tui/style"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/spf13/cobra"
)

func isArtifactPath(path string) (bool, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jar", ".zip", ".pyz", ".mcdr":
	default:
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

func actionLocalInfo(cmd *cobra.Command, filePath string) error {
	infos, err := artifact.Analyze(filePath)
	if err != nil {
		return fmt.Errorf("analyze artifact %q: %w", filePath, err)
	}
	if len(infos) == 0 {
		return fmt.Errorf("no package metadata found in %q", filePath)
	}

	resolution := resolveLocalArtifactUpstream(filePath)

	json, _ := cmd.Flags().GetBool(cli.FlagJSON)
	jsonCompact, _ := cmd.Flags().GetBool(cli.FlagJSONCompact)
	long, _ := cmd.Flags().GetBool(cli.FlagLong)

	if json || jsonCompact {
		views := make([]localArtifactView, len(infos))
		for i, info := range infos {
			views[i] = localArtifactViewFromInfo(info, resolution)
		}
		if len(views) == 1 {
			if jsonCompact {
				style.PrintAsJsonCompact(views[0])
			} else {
				style.PrintAsJson(views[0])
			}
			return nil
		}
		if jsonCompact {
			style.PrintAsJsonCompact(views)
		} else {
			style.PrintAsJson(views)
		}
		return nil
	}

	for i, info := range infos {
		if i > 0 {
			fmt.Println()
		}
		fmt.Print(renderLocalArtifactInfo(info, resolution, long))
	}
	return nil
}

type localArtifactResolution struct {
	Upstream *localArtifactUpstream
	Warnings []string
}

type localArtifactUpstream struct {
	Ref      types.FullPackageRef
	Metadata *types.Metadata
}

func resolveLocalArtifactUpstream(filePath string) localArtifactResolution {
	ref, _, hashErrors := routing.ResolveArtifactByHash(
		artifacthash.File{Path: filePath},
	)
	resolution := localArtifactResolution{
		Warnings: make([]string, 0, len(hashErrors)+1),
	}
	for _, providerErr := range hashErrors {
		resolution.Warnings = append(resolution.Warnings, providerErr.Error())
	}
	if ref.Name == "" {
		resolution.Warnings = append(
			resolution.Warnings,
			"no upstream project matched this artifact hash",
		)
		return resolution
	}

	resolution.Upstream = &localArtifactUpstream{Ref: ref}
	providers, err := routing.ResolveInfoProviders(ref.Eco, ref.Scope)
	if err != nil {
		resolution.Warnings = append(
			resolution.Warnings,
			fmt.Sprintf("cannot retrieve upstream metadata for %s: %v", ref.StringFull(), err),
		)
		return resolution
	}

	metadata, infoErrors, err := routing.GetInfoHedged(
		providers,
		ref.PackageRef,
	)
	for _, providerErr := range infoErrors {
		resolution.Warnings = append(resolution.Warnings, providerErr.Error())
	}
	if err != nil {
		if len(infoErrors) == 0 {
			resolution.Warnings = append(
				resolution.Warnings,
				fmt.Sprintf("cannot retrieve upstream metadata for %s: %v", ref.StringFull(), err),
			)
		}
		return resolution
	}
	metadata.From = ref.Scope
	resolution.Upstream.Metadata = &metadata
	return resolution
}

type localArtifactView struct {
	File         string                     `json:"file"`
	Package      string                     `json:"package"`
	Platform     string                     `json:"platform"`
	Version      string                     `json:"version,omitempty"`
	Dependencies []localArtifactDependency  `json:"dependencies,omitempty"`
	Metadata     types.Metadata             `json:"metadata"`
	FileUpstream *localArtifactUpstreamView `json:"file_upstream,omitempty"`
	Warnings     []string                   `json:"warnings,omitempty"`
}

type localArtifactUpstreamView struct {
	Ref      string          `json:"ref"`
	Metadata *types.Metadata `json:"metadata,omitempty"`
}

type localArtifactDependency struct {
	Package    string               `json:"package"`
	Constraint string               `json:"constraint"`
	Mandatory  bool                 `json:"mandatory"`
	Type       types.DependencyType `json:"type"`
}

func localArtifactViewFromInfo(
	info artifact.Info,
	resolution localArtifactResolution,
) localArtifactView {
	dependencies := make(
		[]localArtifactDependency,
		0,
		len(info.Dependencies),
	)
	for _, dependency := range info.Dependencies {
		dependencies = append(dependencies, localArtifactDependency{
			Package:    formatArtifactRef(dependency.Ref),
			Constraint: formatArtifactConstraint(dependency.Constraint),
			Mandatory:  dependency.Mandatory,
			Type:       types.NormalizeDependencyType(dependency.Type),
		})
	}

	view := localArtifactView{
		File:         info.FilePath,
		Package:      formatArtifactRef(info.Ref),
		Platform:     info.Ref.Eco.String(),
		Version:      info.Version.String(),
		Dependencies: dependencies,
		Metadata:     localArtifactMetadata(info),
		Warnings:     resolution.Warnings,
	}
	if resolution.Upstream != nil {
		upstream := &localArtifactUpstreamView{
			Ref: resolution.Upstream.Ref.StringFull(),
		}
		if artifactMatchesUpstream(info, resolution) {
			upstream.Metadata = resolution.Upstream.Metadata
		}
		view.FileUpstream = upstream
	}
	return view
}

func localArtifactMetadata(info artifact.Info) types.Metadata {
	metadata := info.Metadata
	metadata.From = types.SourceLocal
	if metadata.Title == "" {
		metadata.Title = info.Ref.Name.Title()
	}
	return metadata
}

func artifactMatchesUpstream(
	info artifact.Info,
	resolution localArtifactResolution,
) bool {
	return resolution.Upstream != nil &&
		info.Ref == resolution.Upstream.Ref.PackageRef
}

func renderLocalArtifactInfo(
	info artifact.Info,
	resolution localArtifactResolution,
	longOutput bool,
) string {
	metadata := localArtifactMetadata(info)
	remoteName := info.FilePath
	if resolution.Upstream != nil &&
		resolution.Upstream.Metadata != nil &&
		artifactMatchesUpstream(info, resolution) {
		metadata = *resolution.Upstream.Metadata
		metadata.From = resolution.Upstream.Ref.Scope
		if metadata.Title == "" {
			metadata.Title = localArtifactMetadata(info).Title
		}
		remoteName = resolution.Upstream.Ref.Name.String()
	}

	var out strings.Builder
	out.WriteString(renderInfo(metadata, remoteName, longOutput))
	out.WriteString("\n")
	out.WriteString(renderLocalArtifactDetails(info, resolution))
	if dependencies := renderLocalArtifactDependencies(info.Dependencies); dependencies != "" {
		out.WriteString("\n\n")
		out.WriteString(dependencies)
	}
	if warnings := renderLocalArtifactWarnings(resolution.Warnings); warnings != "" {
		out.WriteString("\n\n")
		out.WriteString(warnings)
	}
	out.WriteString("\n")
	return out.String()
}

func renderLocalArtifactDetails(
	info artifact.Info,
	resolution localArtifactResolution,
) string {
	rows := [][]string{
		{"File", info.FilePath},
		{"Package", formatArtifactRef(info.Ref)},
		{"Platform", info.Ref.Eco.Title()},
		{"Version", info.Version.String()},
	}
	if resolution.Upstream != nil {
		rows = append(
			rows,
			[]string{"File upstream", resolution.Upstream.Ref.StringFull()},
		)
	}
	if info.Compatibility.FoliaSupported {
		rows = append(rows, []string{"Compatibility", "Folia supported"})
	}
	return renderLocalArtifactTable("ARTIFACT", rows)
}

func renderLocalArtifactWarnings(warnings []string) string {
	if len(warnings) == 0 {
		return ""
	}
	lines := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		lines = append(lines, style.Warning("  Warning: "+warning))
	}
	return strings.Join(lines, "\n")
}

func renderLocalArtifactDependencies(dependencies []artifact.Dependency) string {
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
				string(types.NormalizeDependencyType(dependency.Type)),
				formatArtifactConstraint(dependency.Constraint),
			}, " · "),
		})
	}
	return renderLocalArtifactTable("DEPENDENCIES", rows)
}

func renderLocalArtifactTable(title string, rows [][]string) string {
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

func formatArtifactRef(ref types.PackageRef) string {
	if ref.Eco == types.EcoUnspecified {
		return ref.Name.String()
	}
	return ref.Eco.String() + "/" + ref.Name.String()
}

func formatArtifactConstraint(expr types.VersionExpr) string {
	if len(expr) == 0 {
		return "any"
	}

	alternatives := make([]string, 0, len(expr))
	for _, conjunction := range expr {
		terms := make([]string, 0, len(conjunction))
		for _, term := range conjunction {
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
