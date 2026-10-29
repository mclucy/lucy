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

// cacheLsJSON is the machine-readable `cache ls` result: every cached entry.
var cacheLsJSON JSONResult[[]*cache.Entry]

// cacheClearJSON is the machine-readable `cache clear` result.
var cacheClearJSON JSONResult[cache.ResetReport]

func NewCacheCommand(
	contracts *JSONContracts,
	inv *Invocation,
	deps *Dependencies,
) *cobra.Command {
	cacheLsCmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   help.CacheLsShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionCacheLs(cmd, inv, deps, args)
		},
	}
	cacheClearCmd := &cobra.Command{
		Use:     "clear",
		Aliases: []string{"rm"},
		Short:   help.CacheClearShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionCacheClear(cmd, inv, deps, args)
		},
	}
	cacheCmd := &cobra.Command{
		Use:   "cache",
		Short: help.CacheShort,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cacheLsJSON.Bind(contracts, cacheLsCmd)
	cacheClearJSON.Bind(contracts, cacheClearCmd)
	cacheCmd.AddCommand(cacheLsCmd, cacheClearCmd)
	return cacheCmd
}

func actionCacheLs(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	_ []string,
) error {
	output := NewQueryOutput(cmd, inv)
	entries := deps.Network.All()
	if output.JSON() {
		return cacheLsJSON.Write(output, entries)
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
				inv.Theme.Muted(style.FormatTTL(entry.Expiration)),
			),
		})
	}

	_, err := fmt.Fprintln(
		output.Human(),
		terminal.RenderFacts(facts, inv.Stdout.Width, inv.Theme),
	)
	return err
}

func actionCacheClear(
	cmd *cobra.Command,
	inv *Invocation,
	deps *Dependencies,
	_ []string,
) error {
	output := NewOperationOutput(cmd, inv)
	cleared, err := deps.Network.ClearAll()
	if err != nil {
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	if output.JSON() {
		return cacheClearJSON.Write(output, cleared)
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
