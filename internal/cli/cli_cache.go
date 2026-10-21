package cli

import (
	"fmt"
	"slices"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/terminal"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: help.CacheShort,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var cacheLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   help.CacheLsShort,
	RunE:    actionCacheLs,
}

var cacheClearCmd = &cobra.Command{
	Use:     "clear",
	Aliases: []string{"rm"},
	Short:   help.CacheClearShort,
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
		if err := style.WriteJSON(cmd.OutOrStdout(), entries, false); err != nil {
			return err
		}
		return nil
	case JSONCompact:
		if err := style.WriteJSON(cmd.OutOrStdout(), entries, true); err != nil {
			return err
		}
		return nil
	}

	if len(entries) == 0 {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "Cache is empty")
		return err
	}

	slices.SortFunc(entries, func(a, b *cache.Entry) int {
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
				Annotation: style.FormatTTL(entry.Expiration),
			},
		)
	}

	terminal.Flush(out)
	return nil
}

func actionCacheClear(cmd *cobra.Command, _ []string) error {
	cleared, err := cache.Network().ClearAll()
	if err != nil {
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	// The cache backend only collects the successful removals; the command
	// presents them, in the order the reset walked the cache directory.
	out := cmd.OutOrStdout()
	for _, removed := range cleared.Removed {
		if _, err := fmt.Fprintf(out, "removed %s\n", removed); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(
		out,
		"all cache items cleared\nremoved %d files, freed up %s of space\n",
		cleared.FileCount,
		style.FormatBytesBinary(cleared.TotalFreedSize),
	)
	return err
}
