package cli

import (
	"fmt"
	"slices"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/internal/cli/message"
	"github.com/mclucy/lucy/report"
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
	RunE:    actionCacheLs,
}

var cacheClearCmd = &cobra.Command{
	Use:     "clear",
	Aliases: []string{"rm"},
	Short:   message.CacheClearShort,
	RunE:    actionCacheClear,
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
		report.Info("Cache is empty")
		return nil
	}

	slices.SortFunc(entries, func(a, b *cache.CacheEntry) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})

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
	cleared, err := cache.Network().ClearAll()
	if err != nil {
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	// The cache backend only collects the successful removals; the command
	// presents them, in the order the reset walked the cache directory.
	for _, removed := range cleared.Removed {
		report.Info(fmt.Sprintf("removed %s", removed))
	}
	report.Info("all cache items cleared")
	report.Info(
		fmt.Sprintf(
			"removed %d files, freed up %s of space",
			cleared.FileCount,
			style.FormatBytesBinary(cleared.TotalFreedSize),
		),
	)
	return nil
}
