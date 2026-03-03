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

	"github.com/urfave/cli/v3"
)

var subcmdInfo = &cli.Command{
	Name:  "info",
	Usage: "Display information of a mod or plugin",
	Flags: []cli.Flag{
		flagSource,
		flagJsonOutput,
		flagLongOutput,
		flagNoStyle,
	},
	Action: tools.Decorate(
		actionInfo,
		decoratorGlobalFlags,
		decoratorHelpAndExitOnNoArg,
		decoratorLogAndExitOnError,
	),
}

var actionInfo cli.ActionFunc = func(
	ctx context.Context,
	cmd *cli.Command,
) error {
	id := syntax.Parse(cmd.Args().First())
	p := id.NewPackage()
	sourceArg := cmd.String(flagSourceName)
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

	p.Information, p.Remote = &infoResult.Information, &infoResult.Remote
	out = infoOutput(&p, cmd.Bool(flagLongOutput.Name))

	if cmd.Bool(flagJsonOutput.Name) {
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
	maxLines := tools.Ternary[int](
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
					MaxColumns:    tools.TermWidth() * 8 / 10,
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
