package cmd

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/logger"
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

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatedAt.After(entries[j].CreatedAt)
	})

	out := &tui.Data{Fields: []tui.Field{
		&tui.FieldAnnotation{
			Annotation: fmt.Sprintf("(%d entries)", len(entries)),
		},
	}}

	for _, entry := range entries {
		out.Fields = append(out.Fields, &tui.FieldAnnotatedShortText{
			Title:      entry.Key,
			Text:       fmt.Sprintf("%s  %s", entry.Kind, formatSize(entry.Size)),
			Annotation: formatExpiry(entry.Expiration),
		})
	}

	tui.Flush(out)
	return nil
}

var actionCacheClear cli.ActionFunc = func(
	_ context.Context,
	cmd *cli.Command,
) error {
	if err := cache.Network().ClearAll(); err != nil {
		return fmt.Errorf("failed to clear cache: %w", err)
	}
	logger.ShowInfo("Cache cleared")
	return nil
}

func formatSize(bytes int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case bytes >= gb:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(gb))
	case bytes >= mb:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(mb))
	case bytes >= kb:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(kb))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func formatExpiry(t time.Time) string {
	remaining := time.Until(t)
	if remaining <= 0 {
		return "expired"
	}
	switch {
	case remaining >= 24*time.Hour:
		days := int(remaining.Hours() / 24)
		return fmt.Sprintf("expires in %dd", days)
	case remaining >= time.Hour:
		return fmt.Sprintf("expires in %dh", int(remaining.Hours()))
	default:
		return fmt.Sprintf("expires in %dm", int(remaining.Minutes()))
	}
}
