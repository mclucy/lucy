package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/tui"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"

	"github.com/urfave/cli/v3"
)

const (
	flagIndexName  = "index"
	flagClientName = "client"
)

var subcmdSearch = &cli.Command{
	Name:  "search",
	Usage: "Search for mods and plugins",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    flagIndexName,
			Aliases: []string{"i"},
			Usage:   "Index search results by `INDEX`",
			Value:   "relevance",
			Validator: func(s string) error {
				if types.SearchSort(s).Valid() {
					return nil
				}
				return errors.New("must be one of \"relevance\", \"downloads\",\"newest\"")
			},
		},
		&cli.BoolFlag{
			Name:    flagClientName,
			Aliases: []string{"c"},
			Usage:   "Also show client-only mods in results",
			Value:   false,
		},
		flagJsonOutput,
		flagLongOutput,
		flagNoStyle,
		flagSource,
	},
	ArgsUsage: "<platform/name>",
	Action: tools.Decorate(
		actionSearch,
		decoratorGlobalFlags,
		decoratorHelpAndExitOnNoArg,
		decoratorLogAndExitOnError,
	),
	ShellComplete: func(ctx context.Context, cmd *cli.Command) {
		if len(os.Args) < 2 {
			return
		}

		lastArg := ""
		for i := len(os.Args) - 1; i >= 0; i-- {
			if os.Args[i] != "--generate-shell-completion" {
				lastArg = os.Args[i]
				break
			}
		}

		switch lastArg {
		case "--" + flagIndexName, "-i":
			PrintCandidates(StaticSortCandidates())
			return
		case "--" + flagSourceName, "-s":
			PrintCandidates(StaticSourceCandidates())
			return
		}

		prefix := ""
		if cmd.NArg() > 0 {
			prefix = cmd.Args().First()
		}
		all := AggregatePackageCandidates()
		filtered := FilterByPrefix(all, prefix)
		// Truncate to max 50 to avoid overwhelming the shell
		const maxCandidates = 50
		if len(filtered) > maxCandidates {
			filtered = filtered[:maxCandidates]
		}
		PrintCandidates(filtered)
	},
}

var actionSearch cli.ActionFunc = func(
	_ context.Context,
	cmd *cli.Command,
) error {
	p := syntax.Parse(cmd.Args().First())
	options := types.SearchOptions{
		IncludeClient: cmd.Bool(flagClientName),
		SortBy:        types.SearchSort(cmd.String(flagIndexName)),
	}
	sourceArg := cmd.String(flagSourceName)
	specifiedSource := types.ParseSource(sourceArg)

	out := &tui.Data{}
	providers, err := routing.ResolveProviders(p.Platform, specifiedSource)
	if err != nil {
		errArg := sourceArg
		if specifiedSource == types.SourceAuto {
			errArg = p.Platform.String()
		}
		logger.Fatal(fmt.Errorf("%w: %s", err, errArg))
	}

	results, errs := routing.SearchMany(providers, p.Name, options)
	for _, err := range errs {
		if specifiedSource == types.SourceAuto && len(providers) > 1 {
			logger.ReportWarn(
				fmt.Errorf(
					"search on %s failed: %w",
					err.Source.Title(),
					err.Err,
				),
			)
			continue
		}
	}

	for _, res := range results {
		appendToSearchOutput(out, cmd.Bool(flagLongName), res)
	}

	tui.Flush(out)
	return nil
}

func appendToSearchOutput(
	out *tui.Data,
	showAll bool,
	res types.SearchResults,
) {
	var results []string
	for _, r := range res.Projects {
		results = append(results, r.String())
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

	if res.Source == types.SourceModrinth && len(res.Projects) == 100 {
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
			Text:  strconv.Itoa(len(res.Projects)),
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
