//go:build debug

package cmd

import (
	"github.com/mclucy/lucy/util"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(downloadCmd)
}

var downloadCmd = &cobra.Command{
	Use:   "download",
	Short: "Download a specified url (for debugging only)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		url := args[0]

		result, err := util.CachedDownload(url, ".", util.DownloadOptions{})
		if err != nil {
			return err
		}

		println("downloaded", result.File.Name())
		if result.CacheHit {
			println("Cache hit")
		} else {
			println("Cache miss")
		}

		return nil
	},
}
