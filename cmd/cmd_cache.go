package cmd

import (
	"context"
	"fmt"
	"sort"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/slugmap"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/tui"

	"github.com/urfave/cli/v3"
)

var subcmdCache = &cli.Command{
	Name:  "cache",
	Usage: "Manage the download cache",
	Action: tools.Decorate(
		actionEmpty,
		decoratorGlobalFlags,
		decoratorHelpAndExitOnNoArg,
		decoratorHelpAndExitOnError,
	),
	Commands: []*cli.Command{
		subcmdCacheLs,
		subcmdCacheClear,
		subcmdCacheSlugs,
	},
	DefaultCommand: "help",
}

var subcmdCacheLs = &cli.Command{
	Name:    "ls",
	Aliases: []string{"list"},
	Usage:   "List cached entries",
	Flags: []cli.Flag{
		flagJsonOutput,
		flagNoStyle,
	},
	Action: tools.Decorate(
		actionCacheLs,
		decoratorGlobalFlags,
		decoratorLogAndExitOnError,
	),
}

var subcmdCacheClear = &cli.Command{
	Name:    "clear",
	Aliases: []string{"rm"},
	Usage:   "Clear all cached downloads",
	Flags: []cli.Flag{
		flagNoStyle,
	},
	Action: tools.Decorate(
		actionCacheClear,
		decoratorGlobalFlags,
		decoratorLogAndExitOnError,
	),
}

var subcmdCacheSlugs = &cli.Command{
	Name:  "slugs",
	Usage: "Manage the local slug resolution cache",
	Action: tools.Decorate(
		actionEmpty,
		decoratorGlobalFlags,
		decoratorHelpAndExitOnNoArg,
		decoratorHelpAndExitOnError,
	),
	Commands: []*cli.Command{
		subcmdCacheSlugsLs,
		subcmdCacheSlugsClear,
	},
	Aliases:        []string{"slug"},
	DefaultCommand: "help",
}

var subcmdCacheSlugsLs = &cli.Command{
	Name:    "ls",
	Aliases: []string{"list"},
	Usage:   "List slug mappings",
	Flags:   []cli.Flag{flagJsonOutput, flagNoStyle},
	Action: tools.Decorate(
		actionCacheSlugsLs,
		decoratorGlobalFlags,
		decoratorLogAndExitOnError,
	),
}

var subcmdCacheSlugsClear = &cli.Command{
	Name:    "clear",
	Aliases: []string{"rm"},
	Usage:   "Clear all slug mappings",
	Flags:   []cli.Flag{flagNoStyle},
	Action: tools.Decorate(
		actionCacheSlugsClear,
		decoratorGlobalFlags,
		decoratorLogAndExitOnError,
	),
}

var actionCacheLs cli.ActionFunc = func(
	_ context.Context,
	cmd *cli.Command,
) error {
	entries := cache.Network().All()

	if cmd.Bool(flagJsonName) {
		tools.PrintAsJson(entries)
		return nil
	}

	if len(entries) == 0 {
		logger.ShowInfo("Cache is empty")
		return nil
	}

	sort.Slice(
		entries, func(i, j int) bool {
			return entries[i].CreatedAt.After(entries[j].CreatedAt)
		},
	)

	out := &tui.Data{
		Fields: []tui.Field{
			&tui.FieldAnnotation{
				Annotation: fmt.Sprintf("(%d entries)", len(entries)),
			},
		},
	}

	for _, entry := range entries {
		out.Fields = append(
			out.Fields, &tui.FieldAnnotatedShortText{
				Title: entry.Key,
				Text: fmt.Sprintf(
					"%s  %s",
					entry.Kind,
					tools.FormatBytesBinary(entry.Size),
				),
				Annotation: tools.FormatDuration(entry.Expiration),
			},
		)
	}

	tui.Flush(out)
	return nil
}

var actionCacheClear cli.ActionFunc = func(
	_ context.Context,
	cmd *cli.Command,
) (err error) {
	var report cache.ResetReport
	if report, err = cache.Network().ClearAll(); err != nil {
		return fmt.Errorf("failed to clear cache: %w", err)
	}
	logger.ShowInfo("all cache items cleared")
	logger.ShowInfo(
		fmt.Sprintf(
			"removed %d files, freed up %s of space",
			report.FileCount,
			tools.FormatBytesBinary(report.TotalFreedSize),
		),
	)
	return nil
}

var actionCacheSlugsLs cli.ActionFunc = func(
	_ context.Context,
	cmd *cli.Command,
) error {
	entries := slugmap.Default().All()
	if cmd.Bool(flagJsonName) {
		tools.PrintAsJson(entries)
		return nil
	}
	if len(entries) == 0 {
		logger.ShowInfo("Slug map is empty")
		return nil
	}
	out := &tui.Data{
		Fields: []tui.Field{
			&tui.FieldAnnotation{
				Annotation: fmt.Sprintf("(%d entries)", len(entries)),
			},
		},
	}
	for _, e := range entries {
		shortHash := e.FileHash
		if len(shortHash) > 12 {
			shortHash = shortHash[:12]
		}
		out.Fields = append(out.Fields, &tui.FieldAnnotatedShortText{
			Title:      e.Source.String() + "/" + e.LocalId,
			Text:       e.CanonicalSlug,
			Annotation: shortHash,
		})
	}
	tui.Flush(out)
	return nil
}

var actionCacheSlugsClear cli.ActionFunc = func(
	_ context.Context,
	_ *cli.Command,
) error {
	slugmap.Default().Clear()
	logger.ShowInfo("slug map cleared")
	return nil
}
