package cmd

import (
	"fmt"
	"sort"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/internal/knownpkgs"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/tui"
	"github.com/mclucy/lucy/tui/style"
	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the download cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var cacheLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List cached entries",
	RunE:    runWithErrorLogging(actionCacheLs),
}

var cacheClearCmd = &cobra.Command{
	Use:     "clear",
	Aliases: []string{"rm"},
	Short:   "Clear all cached downloads",
	RunE:    runWithErrorLogging(actionCacheClear),
}

var cacheSlugsCmd = &cobra.Command{
	Use:     "slugs",
	Aliases: []string{"slug"},
	Short:   "Manage the local slug resolution cache",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var cacheSlugsLsCmd = &cobra.Command{
	Use:     "ls",
	Aliases: []string{"list"},
	Short:   "List slug mappings",
	RunE:    runWithErrorLogging(actionCacheSlugsLs),
}

var cacheSlugsClearCmd = &cobra.Command{
	Use:     "clear",
	Aliases: []string{"rm"},
	Short:   "Clear all slug mappings",
	RunE:    runWithErrorLogging(actionCacheSlugsClear),
}

func init() {
	addJsonFlag(cacheLsCmd)
	addJsonCompactFlag(cacheLsCmd)
	addNoStyleFlag(cacheLsCmd)

	addNoStyleFlag(cacheClearCmd)

	addJsonFlag(cacheSlugsLsCmd)
	addJsonCompactFlag(cacheSlugsLsCmd)
	addNoStyleFlag(cacheSlugsLsCmd)

	addNoStyleFlag(cacheSlugsClearCmd)

	cacheCmd.AddCommand(cacheLsCmd, cacheClearCmd, cacheSlugsCmd)
	cacheSlugsCmd.AddCommand(cacheSlugsLsCmd, cacheSlugsClearCmd)
	rootCmd.AddCommand(cacheCmd)
}

func actionCacheLs(cmd *cobra.Command, _ []string) error {
	entries := cache.Network().All()
	jsonOutput, _ := cmd.Flags().GetBool(flagJsonName)
	jsonCompact, _ := cmd.Flags().GetBool(flagJsonCompactName)

	if jsonOutput || jsonCompact {
		if jsonCompact {
			style.PrintAsJsonCompact(entries)
		} else {
			style.PrintAsJson(entries)
		}
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
					style.FormatBytesBinary(entry.Size),
				),
				Annotation: style.FormatDuration(entry.Expiration),
			},
		)
	}

	tui.Flush(out)
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

func actionCacheSlugsLs(cmd *cobra.Command, _ []string) error {
	entries := knownpkgs.Default().All()
	jsonOutput, _ := cmd.Flags().GetBool(flagJsonName)
	jsonCompact, _ := cmd.Flags().GetBool(flagJsonCompactName)

	if jsonOutput || jsonCompact {
		if jsonCompact {
			style.PrintAsJsonCompact(entries)
		} else {
			style.PrintAsJson(entries)
		}
		return nil
	}

	if len(entries) == 0 {
		log.ShowInfo("Slug map is empty")
		return nil
	}

	out := &tui.Data{
		Fields: []tui.Field{
			&tui.FieldAnnotation{
				Annotation: fmt.Sprintf("(%d entries)", len(entries)),
			},
		},
	}

	for _, entry := range entries {
		shortHash := entry.FileHash
		if len(shortHash) > 12 {
			shortHash = shortHash[:12]
		}

		out.Fields = append(
			out.Fields, &tui.FieldAnnotatedShortText{
				Title:      entry.Source.String() + "/" + entry.LocalId,
				Text:       entry.CanonicalId,
				Annotation: shortHash,
			},
		)
	}

	tui.Flush(out)
	return nil
}

func actionCacheSlugsClear(_ *cobra.Command, _ []string) error {
	knownpkgs.Default().Clear()
	log.ShowInfo("slug map cleared")
	return nil
}
