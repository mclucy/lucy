package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/tui"
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
		if !types.SearchSort(index).Valid() {
			return errors.New("--index must be one of \"relevance\", \"downloads\", \"newest\"")
		}
		platform, _ := cmd.Flags().GetString(flagPlatformName)
		if platform != "" && !types.PlatformId(platform).IsSearchPlatform() {
			return errors.New("--platform must be one of \"fabric\", \"forge\", \"neoforge\", \"bukkit\"")
		}
		return validateSourceFlag(cmd)
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
	addLongFlag(searchCmd)
	addNoStyleFlag(searchCmd)
	addSourceFlag(searchCmd)
	_ = searchCmd.RegisterFlagCompletionFunc(
		flagSourceName,
		func(cmd *cobra.Command, args []string, toComplete string) (
			[]string,
			cobra.ShellCompDirective,
		) {
			candidates := FilterByPrefix(StaticSourceCandidates(), toComplete)
			return ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
		},
	)
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
				StaticSearchPlatformCandidates(),
				toComplete,
			)
			return ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
		},
	)
	rootCmd.AddCommand(searchCmd)
}

func actionSearch(cmd *cobra.Command, args []string) error {
	p, err := input.Parse(args[0])
	if err != nil {
		logger.Fatal(err)
	}
	index, _ := cmd.Flags().GetString(flagIndexName)
	client, _ := cmd.Flags().GetBool(flagClientName)
	long, _ := cmd.Flags().GetBool(flagLongName)
	sourceArg, _ := cmd.Flags().GetString(flagSourceName)
	platformArg, _ := cmd.Flags().GetString(flagPlatformName)
	specifiedSource := types.ParseSource(sourceArg)

	resolvedPlatform, err := ResolvePlatform(p.Platform, platformArg)
	if err != nil {
		logger.Fatal(err)
	}

	options := types.SearchOptions{
		IncludeClient:  client,
		SortBy:         types.SearchSort(index),
		FilterPlatform: resolvedPlatform,
	}

	out := &tui.Data{}
	providers, err := routing.ResolveSearchProviders(
		options.FilterPlatform,
		specifiedSource,
	)
	if err != nil {
		errArg := sourceArg
		if specifiedSource == types.SourceAuto {
			errArg = options.FilterPlatform.String()
		}
		logger.Fatal(fmt.Errorf("%w: %s", err, errArg))
	}

	results, errs := routing.SearchMany(providers, p.Name, options)
	for _, err := range errs {
		providerErr := fmt.Errorf(
			"search on %s failed: %w",
			err.Source.Title(),
			err.Err,
		)
		if specifiedSource == types.SourceAuto && len(providers) > 1 {
			logger.ReportWarn(providerErr)
			continue
		}
		logger.ReportWarn(providerErr)
	}

	if err := searchResultError(results, errs); err != nil {
		return err
	}

	for _, res := range results {
		appendToSearchOutput(out, long, res)
	}

	tui.Flush(out)
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
		joined = append(joined, providerErr)
	}
	return errors.Join(joined...)
}

func appendToSearchOutput(
	out *tui.Data,
	showAll bool,
	res upstream.SearchResponse,
) {
	var results []string
	for _, r := range res.Items {
		results = append(results, r.RemoteName)
	}

	if len(out.Fields) != 0 {
		out.Fields = append(
			out.Fields, &tui.FieldSeparator{
				Length: 0,
				Dim:    false,
			},
		)
	}

	out.Fields = append(
		out.Fields,
		&tui.FieldAnnotation{
			Annotation: "Results from " + res.Source.Title(),
		},
	)

	if res.Source == types.SourceModrinth && len(res.Items) == 100 {
		out.Fields = append(
			out.Fields,
			&tui.FieldAnnotation{
				Annotation: "* only showing the top 100",
			},
		)
	}

	out.Fields = append(
		out.Fields,
		&tui.FieldShortText{
			Title: "#  ",
			Text:  strconv.Itoa(len(res.Items)),
		},
		&tui.FieldDynamicColumnLabels{
			Title:  ">>>",
			Labels: results,
			MaxLines: tools.Ternary(
				showAll,
				0,
				tools.TermHeight()-6,
			),
		},
	)
}
