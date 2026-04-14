package cmd

import (
	"context"
	"fmt"
	"slices"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/syntax"
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
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= 1 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return CompletePackageIDSuggestions(context.Background(), "info", toComplete)
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
	_ = infoCmd.RegisterFlagCompletionFunc(flagSourceName, func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		candidates := FilterByPrefix(StaticSourceCandidates(), toComplete)
		return ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
	})
	rootCmd.AddCommand(infoCmd)
}

func actionInfo(cmd *cobra.Command, args []string) error {
	id, err := syntax.Parse(args[0])
	if err != nil {
		logger.Fatal(err)
	}
	p := id.NewPackage()
	sourceArg, _ := cmd.Flags().GetString(flagSourceName)
	specifiedSource := types.ParseSource(sourceArg)

	var out *tui.Data

	providers, err := routing.ResolveProviders(id.Platform, specifiedSource)
	if err != nil {
		errArg := sourceArg
		if specifiedSource == types.SourceAuto {
			errArg = id.Platform.String()
		}
		logger.ReportError(fmt.Errorf("%w: %s", err, errArg))
		return err
	}

	infoResult, providerErrors, err := routing.FirstInfo(providers, id)
	for _, providerErr := range providerErrors {
		logger.ReportWarn(
			fmt.Errorf(
				"info on %s failed: %w",
				providerErr.Source.Title(),
				providerErr.Err,
			),
		)
	}

	if err != nil {
		logger.Fatal(fmt.Errorf("failed to get information: %w", err))
	}

	p.Information, p.Remote = &infoResult.Information, &infoResult.Fetch.Remote
	long, _ := cmd.Flags().GetBool(flagLongName)
	out = infoOutput(&p, long)

	jsonOut, _ := cmd.Flags().GetBool(flagJsonName)
	if jsonOut {
		tools.PrintAsJson(p)
	} else {
		tui.Flush(out)
	}
	return nil
}

// TODO: Link to newest version
// TODO: Link to latest compatible version
// TODO: Generate `lucy add` command

func infoOutput(p *types.Package, longOutput bool) *tui.Data {
	maxLines := tools.Ternary(
		longOutput,
		0,
		tools.TermHeight()*3/2,
	)
	useAlternate := !longOutput
	o := &tui.Data{
		Fields: []tui.Field{
			&tui.FieldAnnotation{
				Annotation: "(from " + p.Remote.Source.Title() + ")",
			},
			&tui.FieldShortText{
				Title: "Name",
				Text:  p.Information.Title,
			},
			&tui.FieldShortText{
				Title: "Description",
				Text:  p.Information.Brief,
			},
			tools.Ternary[tui.Field](
				p.Information.DescriptionIsMarkdown,
				&tui.FieldMarkdown{
					Title:         "Information",
					Text:          p.Information.Description,
					Padding:       true,
					LineWrap:      true,
					MaxColumns:    min(tools.TermWidth()*8/10, 100),
					MaxLines:      maxLines,
					UseAlternate:  useAlternate,
					AlternateText: tools.Underline(p.Information.DescriptionUrl),
					FoldNotice:    "",
				},
				&tui.FieldLongText{
					Title:         "Information",
					Text:          p.Information.Description,
					Padding:       true,
					LineWrap:      true,
					MaxColumns:    tools.TermWidth() * 8 / 10,
					MaxLines:      maxLines,
					UseAlternate:  useAlternate,
					AlternateText: tools.Underline(p.Information.DescriptionUrl),
				},
			),
		},
	}

	var authorNames []string
	var authorLinks []string
	for _, author := range p.Information.Authors {
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

	if p.Information != nil {
		o.Fields = append(
			o.Fields,
			&tui.FieldShortText{
				Title: "License",
				Text:  p.Information.License,
			},
		)
	}

	for _, url := range p.Information.Urls {
		o.Fields = append(
			o.Fields, &tui.FieldShortText{
				Title: url.Name,
				Text:  tools.Underline(url.Url),
			},
		)
	}

	o.Fields = append(
		o.Fields, &tui.FieldAnnotatedShortText{
			Title:      "Download",
			Text:       tools.Underline(p.Remote.FileUrl),
			Annotation: p.Remote.Filename,
		},
	)

	// TODO: Put current server version on the top
	// TODO: Hide snapshot versions, except if the current server is using it
	if p.Supports != nil &&
		p.Supports.Platforms != nil &&
		!slices.Contains(p.Supports.Platforms, types.PlatformMCDR) {
		f := &tui.FieldLabels{
			Title:    "Game Versions",
			Labels:   []string{},
			MaxWidth: 0,
			MaxLines: tools.TermHeight() / 2,
		}
		for _, version := range p.Supports.MinecraftVersions {
			f.Labels = append(f.Labels, version.String())
		}
		o.Fields = append(o.Fields, f)
	}

	return o
}
