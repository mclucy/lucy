package cmd

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/tui"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"

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
	PreRunE: func(cmd *cobra.Command, args []string) error {
		return validateSourceFlag(cmd)
	},
	RunE: runWithErrorLogging(actionInfo),
}

func init() {
	addSourceFlag(infoCmd)
	addJsonFlag(infoCmd)
	addLongFlag(infoCmd)
	addNoStyleFlag(infoCmd)
	_ = infoCmd.RegisterFlagCompletionFunc(
		flagSourceName,
		func(cmd *cobra.Command, args []string, toComplete string) (
			[]string,
			cobra.ShellCompDirective,
		) {
			candidates := FilterByPrefix(StaticSourceCandidates(), toComplete)
			return ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
		},
	)
	rootCmd.AddCommand(infoCmd)
}

func actionInfo(cmd *cobra.Command, args []string) error {
	ref, err := input.ParsePackageRef(args[0])
	if err != nil {
		logger.Fatal(err)
	}

	sourceStr, _ := cmd.Flags().GetString(flagSourceName)
	source := types.ParseSource(sourceStr)

	providers, err := routing.ResolveInfoProviders(ref.Platform, source)
	if err != nil {
		errArg := sourceStr
		if source == types.SourceAuto {
			errArg = ref.Platform.String()
		}
		logger.ReportError(fmt.Errorf("%w: %s", err, errArg))
		return err
	}

	meta, providerErrors, err := routing.GetInfoHedged(providers, ref)
	if err != nil {
		logger.Fatal(fmt.Errorf("failed to get information: %w", err))
	}
	for _, providerErr := range providerErrors {
		logger.ReportWarn(
			fmt.Errorf(
				"info on %s failed: %w",
				providerErr.Source.Title(),
				providerErr.Err,
			),
		)
	}

	json, _ := cmd.Flags().GetBool(flagJsonName)
	long, _ := cmd.Flags().GetBool(flagLongName)

	if json {
		tools.PrintAsJson(meta)
	} else {
		var out *tui.Data
		out = infoOutput(meta, long)
		tui.Flush(out)
	}
	return nil
}

func infoOutput(data types.Metadata, longOutput bool) *tui.Data {
	maxLines := tools.Ternary(
		longOutput,
		0,
		tools.TermHeight()*3/2,
	)
	useAlternate := !longOutput
	o := &tui.Data{
		Fields: []tui.Field{
			&tui.FieldAnnotation{
				Annotation: "(from " + data.From.Title() + ")",
			},
			&tui.FieldShortText{
				Title: "Name",
				Text:  data.Title,
			},
			&tui.FieldShortText{
				Title: "Description",
				Text:  data.Brief,
			},
			tools.Ternary[tui.Field](
				data.DescriptionIsMarkdown,
				&tui.FieldMarkdown{
					Title:         "Information",
					Text:          data.Description,
					Padding:       true,
					LineWrap:      true,
					MaxColumns:    min(tools.TermWidth()*8/10, 100),
					MaxLines:      maxLines,
					UseAlternate:  useAlternate,
					AlternateText: tools.Underline(data.DescriptionUrl),
					FoldNotice:    "",
				},
				&tui.FieldLongText{
					Title:         "Information",
					Text:          data.Description,
					Padding:       true,
					LineWrap:      true,
					MaxColumns:    tools.TermWidth() * 8 / 10,
					MaxLines:      maxLines,
					UseAlternate:  useAlternate,
					AlternateText: tools.Underline(data.DescriptionUrl),
				},
			),
		},
	}

	var authorNames []string
	var authorLinks []string
	for _, author := range data.Authors {
		authorNames = append(authorNames, author.Name)
		authorLinks = append(authorLinks, author.Url)
	}

	o.Fields = append(
		o.Fields,
		&tui.FieldMultiAnnotatedShortText{
			Title:       "Authors",
			Texts:       authorNames,
			Annotations: authorLinks,
			ShowTotal:   false,
		},
	)

	o.Fields = append(
		o.Fields,
		&tui.FieldShortText{
			Title: "License",
			Text:  data.License,
		},
	)

	for _, url := range data.Urls {
		o.Fields = append(
			o.Fields, &tui.FieldShortText{
				Title: url.Name,
				Text:  tools.Underline(url.Url),
			},
		)
	}

	return o
}
