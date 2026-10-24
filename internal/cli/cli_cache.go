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
	output := NewQueryOutput(cmd)
	entries := cache.Network().All()
	if output.JSON() {
		return output.WriteJSON(entries)
	}

	if len(entries) == 0 {
		_, err := fmt.Fprintln(output.Human(), "Cache is empty")
		return err
	}

	slices.SortFunc(entries, func(a, b *cache.Entry) int {
		return b.CreatedAt.Compare(a.CreatedAt)
	})

	facts := make([]terminal.Fact, 0, len(entries)+1)
	facts = append(facts, terminal.Fact{
		Key:   "Cache",
		Value: fmt.Sprintf("%d entries", len(entries)),
	})
	for _, entry := range entries {
		facts = append(facts, terminal.Fact{
			Key: entry.Key,
			Value: fmt.Sprintf(
				"%s  %s  %s",
				entry.Kind,
				style.FormatBytesBinary(entry.Size),
				style.Muted(style.FormatTTL(entry.Expiration)),
			),
		})
	}

	_, err := fmt.Fprintln(
		output.Human(),
		terminal.RenderFacts(facts, style.TermWidth()),
	)
	return err
}

func actionCacheClear(cmd *cobra.Command, _ []string) error {
	output := NewOperationOutput(cmd)
	cleared, err := cache.Network().ClearAll()
	if err != nil {
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	if output.JSON() {
		return output.WriteJSON(cleared)
	}

	// The cache backend only collects the successful removals; the command
	// presents them, in the order the reset walked the cache directory.
	out := output.Human()
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
