package search

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/spf13/cobra"
)

const (
	flagSortName          = "sort"
	flagIncludeClientName = "include-client"
	flagEcosystemName     = cli.FlagEcosystem
	flagRegistryName      = "registry"
)

func NewCommand(
	contracts *cli.JSONContracts,
	inv *cli.Invocation,
	deps *cli.Dependencies,
) *cobra.Command {
	searchCmd := &cobra.Command{
		Use:   "search",
		Short: help.SearchShort,
		Args:  cobra.ExactArgs(1),
		ValidArgsFunction: func(
			cmd *cobra.Command,
			args []string,
			toComplete string,
		) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			sort, _ := cmd.Flags().GetString(flagSortName)
			if !Sort(sort).Valid() {
				return errors.New("--sort must be one of \"relevance\", \"downloads\", \"newest\"")
			}
			ecosystem, _ := cmd.Flags().GetString(flagEcosystemName)
			if _, err := searchEcosystem(ecosystem); err != nil {
				return err
			}
			registry, _ := cmd.Flags().GetString(flagRegistryName)
			_, err := searchRegistry(registry)
			return err
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionSearch(cmd, inv, deps, args)
		},
	}
	searchJSON.Bind(contracts, searchCmd)
	searchCmd.Flags().String(
		flagSortName,
		"relevance",
		"Sort results by SORT (relevance, downloads, newest)",
	)
	searchCmd.Flags().BoolP(
		flagIncludeClientName,
		"c",
		false,
		"Also show client-only mods in results",
	)
	searchCmd.Flags().String(
		flagEcosystemName,
		"",
		"Filter results by ecosystem (fabric, forge, neoforge, bukkit, paper)",
	)
	searchCmd.Flags().StringP(
		flagRegistryName,
		"s",
		"",
		"Restrict results to REGISTRY",
	)
	cli.AddLongFlag(searchCmd)
	_ = searchCmd.RegisterFlagCompletionFunc(
		flagRegistryName,
		func(cmd *cobra.Command, args []string, toComplete string) (
			[]string,
			cobra.ShellCompDirective,
		) {
			candidates := cli.FilterByPrefix(
				cli.StaticSearchRegistryCandidates(),
				toComplete,
			)
			return cli.ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
		},
	)
	_ = searchCmd.RegisterFlagCompletionFunc(
		flagSortName,
		func(cmd *cobra.Command, args []string, toComplete string) (
			[]string,
			cobra.ShellCompDirective,
		) {
			candidates := cli.FilterByPrefix(StaticSortCandidates(), toComplete)
			return cli.ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
		},
	)
	_ = searchCmd.RegisterFlagCompletionFunc(
		flagEcosystemName,
		func(cmd *cobra.Command, args []string, toComplete string) (
			[]string,
			cobra.ShellCompDirective,
		) {
			candidates := cli.FilterByPrefix(
				cli.StaticSearchEcosystemCandidates(),
				toComplete,
			)
			return cli.ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
		},
	)
	return searchCmd
}

func actionSearch(
	cmd *cobra.Command,
	inv *cli.Invocation,
	deps *cli.Dependencies,
	args []string,
) error {
	ctx, stop := cli.CommandContext(cmd)
	defer stop()
	sortOrder, _ := cmd.Flags().GetString(flagSortName)
	client, _ := cmd.Flags().GetBool(flagIncludeClientName)
	long, _ := cmd.Flags().GetBool(cli.FlagLong)
	ecosystemArg, _ := cmd.Flags().GetString(flagEcosystemName)
	resolvedEcosystem, err := searchEcosystem(ecosystemArg)
	if err != nil {
		return err
	}
	registryArg, _ := cmd.Flags().GetString(flagRegistryName)
	specifiedRegistry, err := searchRegistry(registryArg)
	if err != nil {
		return err
	}

	query := upstream.Query{
		Keyword:         args[0],
		IncludeClient:   client,
		FilterEcosystem: resolvedEcosystem,
	}

	providers, err := deps.Router.ResolveSearchProviders(
		query.FilterEcosystem,
		specifiedRegistry,
	)
	if err != nil {
		errArg := query.FilterEcosystem.String()
		if specifiedRegistry != catalog.RegistryUnspecified {
			errArg = specifiedRegistry.String()
		}
		return fmt.Errorf("%w: %s", err, errArg)
	}

	results, errs := deps.Router.SearchMany(ctx, providers, query)
	if err := ctx.Err(); err != nil {
		return err
	}
	applySearchSort(results, Sort(sortOrder))

	if err := searchResultError(results, errs); err != nil {
		return err
	}

	output := cli.NewQueryOutput(cmd, inv)

	var noResultSources []string
	for _, err := range errs {
		if isNoResultsError(err.Err) {
			noResultSources = append(noResultSources, err.Provider.Title())
			continue
		}
		providerWarn := fmt.Errorf(
			"search on %s failed: %w",
			err.Provider.Title(),
			err.Err,
		)
		if output.JSON() {
			inv.Logger.Warn(providerWarn)
			continue
		}
		if len(results) > 0 {
			inv.Logger.Warn(providerWarn)
		}
		report.New(cmd.ErrOrStderr(), inv.Theme).Warn(providerWarn)
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if output.JSON() {
		return searchJSON.Write(output, newSearchResultDocuments(results))
	}

	var sb strings.Builder
	for i, res := range results {
		if i > 0 {
			sb.WriteString("\n")
		}
		if long {
			sb.WriteString(renderSearchLong(res, inv.Theme, inv.Stdout))
		} else {
			sb.WriteString(renderSearchCompact(res, inv.Theme))
		}
	}
	if len(noResultSources) > 0 {
		sb.WriteString(
			inv.Theme.Muted(
				"No results from "+strings.Join(noResultSources, ", "),
			) + "\n",
		)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fmt.Fprint(output.Human(), sb.String())
	return nil
}

func searchEcosystem(value string) (catalog.Ecosystem, error) {
	if value == "" {
		return catalog.EcosystemUnspecified, nil
	}

	ecosystem := catalog.Ecosystem(strings.ToLower(strings.TrimSpace(value)))
	if !ecosystem.IsSearchable() {
		return catalog.EcosystemUnspecified, fmt.Errorf("invalid --ecosystem %q", value)
	}
	return ecosystem, nil
}

func searchRegistry(value string) (catalog.Registry, error) {
	if value == "" {
		return catalog.RegistryUnspecified, nil
	}

	registry, ok := catalog.ParseRegistry(value)
	if !ok {
		return catalog.RegistryUnknown, fmt.Errorf("invalid --registry %q", value)
	}
	return registry, nil
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

func renderSearchCompact(res upstream.SearchResponse, theme style.Theme) string {
	var sb strings.Builder
	sb.WriteString(searchSectionHeader(res, theme))

	if len(res.Items) == 0 {
		sb.WriteString(theme.Muted("  (no results)") + "\n")
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
		if desc == "" && item.Title != "" && item.Title != item.ProjectName {
			desc = item.Title
		}
		t.Row("  "+item.DisplayName(), truncateText(desc, 60))
	}
	sb.WriteString(t.String())
	sb.WriteString("\n")
	return sb.String()
}

func renderSearchLong(
	res upstream.SearchResponse,
	theme style.Theme,
	terminal style.Terminal,
) string {
	var sb strings.Builder
	sb.WriteString(searchSectionHeader(res, theme))

	if len(res.Items) == 0 {
		sb.WriteString(theme.Muted("  (no results)") + "\n")
		return sb.String()
	}

	for i, item := range res.Items {
		if i > 0 {
			sb.WriteString("\n")
		}
		displayName := item.DisplayName()
		if item.Title != "" && item.Title != item.ProjectName {
			displayName = item.Title
		}
		sb.WriteString("  " + theme.Accent(displayName) + "\n")

		if item.Description != "" {
			desc := strings.ReplaceAll(item.Description, "\n", " ")
			desc = strings.ReplaceAll(desc, "\r", "")
			wrapWidth := min(80, terminal.Width) - 2
			wrapped := lipgloss.Wrap(desc, wrapWidth, "")
			for line := range strings.SplitSeq(wrapped, "\n") {
				sb.WriteString("  " + line + "\n")
			}
		}

		stats := searchStatsLine(item)
		if stats != "" {
			sb.WriteString("  " + theme.Muted(stats) + "\n")
		}

		installID := item.Provider.String() + ":" + item.ProjectName
		sb.WriteString("  " + theme.Muted(installID) + "\n")
	}
	return sb.String()
}

func searchSectionHeader(
	res upstream.SearchResponse,
	theme style.Theme,
) string {
	header := theme.Muted(
		"Results from " + res.Provider.Title() +
			" (" + strconv.Itoa(len(res.Items)) + ")",
	)
	var sb strings.Builder
	sb.WriteString(header + "\n")
	if res.Provider == catalog.ProviderModrinth && len(res.Items) == 100 {
		sb.WriteString(theme.Muted("* only showing the top 100") + "\n")
	}
	return sb.String()
}

func searchStatsLine(item upstream.SearchResult) string {
	var sb strings.Builder
	if item.Downloads > 0 {
		sb.WriteString(formatDownloads(item.Downloads) + " downloads")
	}
	if !item.LastUpdated.IsZero() {
		if sb.Len() > 0 {
			sb.WriteString(", last updated ")
		} else {
			sb.WriteString("Last updated ")
		}
		sb.WriteString(timeAgo(item.LastUpdated))
	}
	return sb.String()
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
