//go:build debug

package cmd

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/util"
	"github.com/urfave/cli/v3"
)

func init() {
	Cli.Commands = append(Cli.Commands, subcmdDownload)
}

var subcmdDownload = &cli.Command{
	Name:  "download",
	Usage: "Download a specified url (for debugging only)",
	Action: func(_ context.Context, cmd *cli.Command) error {
		url := cmd.Args().First()
		if url == "" {
			return fmt.Errorf("url is required")
		}

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
