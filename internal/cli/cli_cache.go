package cli

import (
	"fmt"
	"sort"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/internal/cli/message"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/terminal"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: message.CacheShort,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var cacheLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   message.CacheLsShort,
	RunE:    WithErrorLogging(actionCacheLs),
}

var cacheClearCmd = &cobra.Command{
	Use:     "clear",
	Aliases: []string{"rm"},
	Short:   message.CacheClearShort,
	RunE:    WithErrorLogging(actionCacheClear),
}

func NewCacheCommand() *cobra.Command {
	cacheCmd.AddCommand(cacheLsCmd, cacheClearCmd)
	return cacheCmd
}

func actionCacheLs(cmd *cobra.Command, _ []string) error {
	entries := cache.Network().All()

	switch JSONMode(cmd) {
	case JSONPretty:
		style.PrintAsJson(entries)
		return nil
	case JSONCompact:
		style.PrintAsJsonCompact(entries)
		return nil
	}

	if len(entries) == 0 {
		log.ShowInfo("Cache is empty")
		return nil
	}

	sort.Slice(
		entries, func(i, j int) bool {
			return entries[i].CreatedAt.After(entries[j].CreatedAt)
		},
	)

	out := &terminal.Data{
		Fields: []terminal.Field{
			&terminal.FieldAnnotation{
				Annotation: fmt.Sprintf("(%d entries)", len(entries)),
			},
		},
	}

	for _, entry := range entries {
		out.Fields = append(
			out.Fields, &terminal.FieldAnnotatedShortText{
				Title: entry.Key,
				Text: fmt.Sprintf(
					"%s  %s",
					entry.Kind,
					style.FormatBytesBinary(entry.Size),
				),
				Annotation: style.FormatDuration(entry.Expiration),
			},
		)
	}

	terminal.Flush(out)
	return nil
}

func actionCacheClear(_ *cobra.Command, _ []string) error {
	report, err := cache.Network().ClearAll()
	if err != nil {
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	log.ShowInfo("all cache items cleared")
	log.ShowInfo(
		fmt.Sprintf(
			"removed %d files, freed up %s of space",
			report.FileCount,
			style.FormatBytesBinary(report.TotalFreedSize),
		),
	)
	return nil
}
