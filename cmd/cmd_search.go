package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/tui/style"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/spf13/cobra"
)

const (
	flagIndexName    = "index"
	flagClientName   = "client"
	flagPlatformName = "platform"
)

var searchCmd = &cobra.Command{
	Use:   "search",
	Short: "Search for mods and plugins",
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
			"search",
			toComplete,
		)
	},
	PreRunE: func(cmd *cobra.Command, args []string) error {
		index, _ := cmd.Flags().GetString(flagIndexName)
		if !SearchSort(index).Valid() {
			return errors.New("--index must be one of \"relevance\", \"downloads\", \"newest\"")
		}
		platform, _ := cmd.Flags().GetString(flagPlatformName)
		if platform != "" && !types.Ecosystem(platform).IsSearchEcosystem() {
			return errors.New("--platform must be one of \"fabric\", \"forge\", \"neoforge\", \"bukkit\"")
		}
		return nil
	},
	RunE: runWithErrorLogging(actionSearch),
}

func init() {
	searchCmd.Flags().StringP(
		flagIndexName,
		"i",
		"relevance",
		"Index search results by INDEX",
	)
	searchCmd.Flags().BoolP(
		flagClientName,
		"c",
		false,
		"Also show client-only mods in results",
	)
	searchCmd.Flags().String(
		flagPlatformName,
		"",
		"Filter results by platform (fabric, forge, neoforge, bukkit)",
	)
	addJsonFlag(searchCmd)
	addJsonCompactFlag(searchCmd)
	addLongFlag(searchCmd)
	addNoStyleFlag(searchCmd)
	_ = searchCmd.RegisterFlagCompletionFunc(
		flagIndexName,
		func(cmd *cobra.Command, args []string, toComplete string) (
			[]string,
			cobra.ShellCompDirective,
		) {
			candidates := FilterByPrefix(StaticSortCandidates(), toComplete)
			return ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
		},
	)
	_ = searchCmd.RegisterFlagCompletionFunc(
		flagPlatformName,
		func(cmd *cobra.Command, args []string, toComplete string) (
			[]string,
			cobra.ShellCompDirective,
		) {
			candidates := FilterByPrefix(
				StaticSearchEcosystemCandidates(),
				toComplete,
			)
			return ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
		},
	)
	rootCmd.AddCommand(searchCmd)
}

func actionSearch(cmd *cobra.Command, args []string) error {
	ref, err := input.ParseFullPackageRef(args[0])
	if err != nil {
		return err
	}
	index, _ := cmd.Flags().GetString(flagIndexName)
	client, _ := cmd.Flags().GetBool(flagClientName)
	long, _ := cmd.Flags().GetBool(flagLongName)
	platformArg, _ := cmd.Flags().GetString(flagPlatformName)
	specifiedSource := ref.Scope

	resolvedPlatform, err := ResolveEcosystem(
		ref.PackageRef.Eco,
		platformArg,
	)
	if err != nil {
		return err
	}

	options := upstream.SearchOptions{
		IncludeClient:   client,
		FilterEcosystem: resolvedPlatform,
	}

	providers, err := routing.ResolveSearchProviders(
		options.FilterEcosystem,
		specifiedSource,
	)
	if err != nil {
		errArg := options.FilterEcosystem.String()
		if specifiedSource != types.SourceAuto {
			errArg = specifiedSource.String()
		}
		return fmt.Errorf("%w: %s", err, errArg)
	}

	results, errs := routing.SearchMany(providers, ref.PackageRef.Name, options)
	applySearchSort(results, SearchSort(index))

	var noResultSources []string
	for _, err := range errs {
		if isNoResultsError(err.Err) {
			noResultSources = append(noResultSources, err.Source.Title())
			continue
		}
		providerErr := fmt.Errorf(
			"search on %s failed: %w",
			err.Source.Title(),
			err.Err,
		)
		log.ReportWarn(providerErr)
	}

	if err := searchResultError(results, errs); err != nil {
		return err
	}

	var sb strings.Builder
	for i, res := range results {
		if i > 0 {
			sb.WriteString("\n")
		}
		if long {
			sb.WriteString(renderSearchLong(res))
		} else {
			sb.WriteString(renderSearchCompact(res))
		}
	}
	if len(noResultSources) > 0 {
		sb.WriteString(
			style.Muted(
				"No results from "+strings.Join(noResultSources, ", "),
			) + "\n",
		)
	}
	fmt.Print(sb.String())
	return nil
}

func searchResultError(
	results []upstream.SearchResponse,
	providerErrors []routing.ProviderError,
) error {
	if len(results) > 0 || len(providerErrors) == 0 {
		return nil
	}
	joined := make([]error, 0, len(providerErrors))
	for _, providerErr := range providerErrors {
		if isNoResultsError(providerErr.Err) {
			continue
		}
		joined = append(joined, providerErr)
	}
	if len(joined) == 0 {
		return nil
	}
	return errors.Join(joined...)
}

func isNoResultsError(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "no projects found")
}

// renderSearchCompact renders a source's results as a two-column table:
// slug in the left column, description in the right.
func renderSearchCompact(res upstream.SearchResponse) string {
	var sb strings.Builder
	sb.WriteString(searchSectionHeader(res))

	if len(res.Items) == 0 {
		sb.WriteString(style.Muted("  (no results)") + "\n")
		return sb.String()
	}

	t := table.New().
		Border(lipgloss.HiddenBorder()).
		BorderTop(false).
		BorderBottom(false).
		BorderLeft(false).
		BorderRight(false).
		BorderColumn(false).
		BorderRow(false).
		BorderHeader(false).
		StyleFunc(
			func(row, col int) lipgloss.Style {
				if col == 0 {
					return lipgloss.NewStyle().Bold(true)
				}
				return lipgloss.NewStyle().Faint(true)
			},
		)

	for _, item := range res.Items {
		desc := item.Description
		if desc == "" && item.Title != "" && item.Title != item.RemoteName {
			desc = item.Title
		}
		t.Row("  "+item.FormattedName(), truncateText(desc, 60))
	}
	sb.WriteString(t.String())
	sb.WriteString("\n")
	return sb.String()
}

// renderSearchLong renders a source's results in npm-style multiline format:
// name (bold), description, stats line, install identifier.
func renderSearchLong(res upstream.SearchResponse) string {
	var sb strings.Builder
	sb.WriteString(searchSectionHeader(res))

	if len(res.Items) == 0 {
		sb.WriteString(style.Muted("  (no results)") + "\n")
		return sb.String()
	}

	for i, item := range res.Items {
		if i > 0 {
			sb.WriteString("\n")
		}
		displayName := item.FormattedName()
		if item.Title != "" && item.Title != item.RemoteName {
			displayName = item.Title
		}
		sb.WriteString("  " + style.Accent(displayName) + "\n")

		if item.Description != "" {
			desc := strings.ReplaceAll(item.Description, "\n", " ")
			desc = strings.ReplaceAll(desc, "\r", "")
			wrapWidth := min(80, style.TermWidth()) - 2
			wrapped := lipgloss.Wrap(desc, wrapWidth, "")
			for _, line := range strings.Split(wrapped, "\n") {
				sb.WriteString("  " + line + "\n")
			}
		}

		stats := searchStatsLine(item)
		if stats != "" {
			sb.WriteString("  " + style.Muted(stats) + "\n")
		}

		installID := item.Source.String() + ":" + item.FormattedName()
		sb.WriteString("  " + style.Muted(installID) + "\n")
	}
	return sb.String()
}

func searchSectionHeader(res upstream.SearchResponse) string {
	header := style.Muted(
		"Results from " + res.Source.Title() +
			" (" + strconv.Itoa(len(res.Items)) + ")",
	)
	var sb strings.Builder
	sb.WriteString(header + "\n")
	if res.Source == types.SourceModrinth && len(res.Items) == 100 {
		sb.WriteString(style.Muted("* only showing the top 100") + "\n")
	}
	return sb.String()
}

func searchStatsLine(item upstream.SearchResult) string {
	parts := make([]string, 0, 2)
	if item.Downloads > 0 {
		parts = append(parts, formatDownloads(item.Downloads)+" downloads")
	}
	if !item.LastUpdated.IsZero() {
		parts = append(parts, "updated "+timeAgo(item.LastUpdated))
	}
	return strings.Join(parts, " · ")
}

func formatDownloads(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return strconv.FormatInt(n, 10)
	}
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return strconv.Itoa(m) + " minutes ago"
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return strconv.Itoa(h) + " hours ago"
	case d < 30*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return strconv.Itoa(days) + " days ago"
	case d < 365*24*time.Hour:
		months := int(d.Hours() / 24 / 30)
		if months <= 1 {
			return "1 month ago"
		}
		return strconv.Itoa(months) + " months ago"
	default:
		years := int(d.Hours() / 24 / 365)
		if years == 1 {
			return "1 year ago"
		}
		return strconv.Itoa(years) + " years ago"
	}
}

func truncateText(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
