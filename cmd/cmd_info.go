package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/tui/style"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info",
	Short: "Display information of a mod or plugin",
	Args:  cobra.ExactArgs(1),
	ValidArgsFunction: func(
		cmd *cobra.Command,
		args []string,
		toComplete string,
	) ([]string, cobra.ShellCompDirective) {
		if len(args) >= 1 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return CompletePackageIDSuggestions(
			context.Background(),
			"info",
			toComplete,
		)
	},

	RunE: runWithErrorLogging(actionInfo),
}

func init() {
	addJsonFlag(infoCmd)
	addLongFlag(infoCmd)
	addNoStyleFlag(infoCmd)
	rootCmd.AddCommand(infoCmd)
}

func actionInfo(cmd *cobra.Command, args []string) error {
	ref, err := input.ParseFullPackageRef(args[0])
	if err != nil {
		return err
	}

	source := ref.Scope

	providers, err := routing.ResolveInfoProviders(ref.Eco, source)
	if err != nil {
		errArg := ref.Eco.String()
		if source != types.SourceAuto {
			errArg = source.String()
		}
		return fmt.Errorf("%w: %s", err, errArg)
	}

	meta, providerErrors, err := routing.GetInfoHedged(
		providers,
		ref.PackageRef,
	)
	if err != nil {
		return fmt.Errorf("failed to get information: %w", err)
	}

	var noResultSources []string
	for _, providerErr := range providerErrors {
		if strings.Contains(providerErr.Err.Error(), "not found") {
			noResultSources = append(
				noResultSources,
				providerErr.Source.Title(),
			)
			continue
		}
		log.ReportWarn(
			fmt.Errorf(
				"info on %s failed: %w",
				providerErr.Source.Title(),
				providerErr.Err,
			),
		)
	}

	json, _ := cmd.Flags().GetBool(flagJsonName)
	jsonCompact, _ := cmd.Flags().GetBool(flagJsonCompactName)
	long, _ := cmd.Flags().GetBool(flagLongName)

	if json || jsonCompact {
		if jsonCompact {
			style.PrintAsJsonCompact(meta)
		} else {
			style.PrintAsJson(meta)
		}
	} else {
		output := renderInfo(meta, ref.PackageRef.Name.String(), long)
		if len(noResultSources) > 0 {
			output += style.Muted(
				"  Not found on "+strings.Join(
					noResultSources,
					", ",
				),
			) + "\n"
		}
		fmt.Print(output)
	}
	return nil
}

func renderInfo(
	data types.Metadata,
	remoteName string,
	longOutput bool,
) string {
	var out strings.Builder
	installID := data.From.String() + ":" + remoteName

	out.WriteString(style.Accent(data.Title))
	out.WriteString("\n")
	out.WriteString(style.Muted(installID))
	out.WriteString("\n")
	briefWidth := min(80, style.TermWidth()) - 2
	briefWrapped := lipgloss.Wrap(data.Brief, briefWidth, "")
	out.WriteString(briefWrapped)
	out.WriteString("\n")

	if metadataLine := renderInfoMetadataLine(data); metadataLine != "" {
		out.WriteString("\n")
		out.WriteString(metadataLine)
		out.WriteString("\n")
	}

	if readme := renderInfoReadme(data, longOutput); readme != "" {
		out.WriteString("\n")
		out.WriteString(readme)
		out.WriteString("\n")
	}

	if links := renderInfoLinks(data); links != "" {
		out.WriteString("\n")
		out.WriteString(links)
		out.WriteString("\n")
	}

	return out.String()
}

func renderInfoMetadataLine(data types.Metadata) string {
	parts := make([]string, 0, 2)
	if data.License != "" {
		parts = append(parts, data.License)
	}
	if authors := renderInfoAuthors(data.Authors); authors != "" {
		parts = append(parts, "by "+authors)
	}
	if len(parts) == 0 {
		return ""
	}
	return style.Muted(strings.Join(parts, " | "))
}

func renderInfoAuthors(authors []types.Person) string {
	names := make([]string, 0, len(authors))
	for _, author := range authors {
		if author.Name == "" {
			continue
		}
		names = append(names, author.Name)
	}
	if len(names) == 0 {
		return ""
	}
	if len(names) <= 3 {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, %s, +%d more", names[0], names[1], len(names)-2)
}

func renderInfoReadme(data types.Metadata, longOutput bool) string {
	description := strings.TrimSpace(data.Description)
	if description == "" {
		return ""
	}

	maxWidth := min(style.TermWidth()*8/10, 100)
	maxLines := 0
	if !longOutput {
		maxLines = style.TermHeight() * 3 / 2
	}

	var displayText string
	if data.DescriptionIsMarkdown {
		displayText = style.MarkdownToAnsi(description, maxWidth)
	} else {
		displayText = lipgloss.Wrap(description, maxWidth, "")
	}

	truncatedText, truncated := truncateInfoText(displayText, maxLines)
	totalLines := len(strings.Split(displayText, "\n"))
	displayedLines := len(strings.Split(truncatedText, "\n"))
	truncatedLines := totalLines - displayedLines

	var out strings.Builder
	out.WriteString(
		renderInfoSectionHeader(
			"README",
			displayedLines,
			totalLines,
			maxWidth,
			truncated,
		),
	)
	out.WriteString("\n\n")
	out.WriteString(indentInfoBlock(truncatedText))
	if truncated {
		out.WriteString("\n\n")
		out.WriteString(
			style.Muted(
				fmt.Sprintf("  [... %d lines truncated]", truncatedLines),
			),
		)
		out.WriteString("\n")
		out.WriteString("  ")
		if data.DescriptionUrl != "" {
			out.WriteString(style.Muted("Full README -> "))
			out.WriteString(style.Link(data.DescriptionUrl))
		} else {
			out.WriteString(
				style.Muted(
					fmt.Sprintf(
						"(%d more lines, use --long to expand)",
						totalLines-displayedLines,
					),
				),
			)
		}
	}
	return out.String()
}

func truncateInfoText(text string, maxLines int) (string, bool) {
	if maxLines <= 0 {
		return text, false
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text, false
	}
	return strings.Join(lines[:maxLines], "\n"), true
}

func renderInfoSectionHeader(
	title string,
	lineCount int,
	totalLines int,
	maxWidth int,
	truncated bool,
) string {
	lineCountText := fn.Ternary(
		truncated,
		fmt.Sprintf("(%d/%d lines)", lineCount, totalLines),
		fmt.Sprintf("(%d lines)", lineCount),
	)
	label := fmt.Sprintf("  ─── %s %s ", title, lineCountText)
	if maxWidth <= 0 {
		maxWidth = style.TermWidth()
	}
	labelWidth := lipgloss.Width(label)
	if labelWidth >= maxWidth {
		return style.Muted(label)
	}
	return style.Muted(label) + style.Muted(
		strings.Repeat(
			"─",
			maxWidth-labelWidth,
		),
	)
}

func indentInfoBlock(text string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = "  " + line
	}
	return strings.Join(lines, "\n")
}

func renderInfoLinks(data types.Metadata) string {
	rows := make([][]string, 0, len(data.Urls))
	for _, url := range data.Urls {
		if url.Url == "" {
			continue
		}
		linked := ansi.SetHyperlink(url.Url) + url.Url + ansi.ResetHyperlink()
		rows = append(rows, []string{url.Name, linked})
	}
	if len(rows) == 0 {
		return ""
	}

	return table.New().
		Border(lipgloss.HiddenBorder()).
		Rows(rows...).
		StyleFunc(
			func(_ int, col int) lipgloss.Style {
				switch col {
				case 0:
					return lipgloss.NewStyle().Bold(true)
				default:
					return lipgloss.NewStyle()
				}
			},
		).
		Render()
}
