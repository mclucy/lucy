package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/tui"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"

	"github.com/urfave/cli/v3"
)

var subcmdSearch = &cli.Command{
	Name:  "search",
	Usage: "Search for mods and plugins",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "index",
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
			Name:    "client",
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
}

var actionSearch cli.ActionFunc = func(
	_ context.Context,
	cmd *cli.Command,
) error {
	p := syntax.Parse(cmd.Args().First())

	showClientPackage := cmd.Bool("client")
	indexBy := types.SearchSort(cmd.String("index"))
	options := types.SearchOptions{
		IncludeClient: showClientPackage,
		SortBy:        indexBy,
	}
	sourceArg := cmd.String("source")
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

	results, providerErrors := routing.SearchMany(providers, p.Name, options)

	for _, providerErr := range providerErrors {
		if specifiedSource == types.SourceAuto && len(providers) > 1 {
			logger.ReportWarn(
				fmt.Errorf(
					"search on %s failed: %w",
					providerErr.Source.Title(),
					providerErr.Err,
				),
			)
			continue
		}
		if !errors.Is(providerErr.Err, upstream.ErrorNoResults) {
			logger.Fatal(providerErr.Err)
		}
	}

	for _, res := range results {
		appendToSearchOutput(out, cmd.Bool("long"), res)
	}

	if len(results) == 0 {
		logger.ShowInfo("no results found")
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
