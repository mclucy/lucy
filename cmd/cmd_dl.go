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

		file, hit, err := util.DownloadFileWithCache(url, ".", 0)
		if err != nil {
			return err
		}

		println("downloaded", file.Name())
		if hit {
			println("Cache hit")
		} else {
			println("Cache miss")
		}

		return nil
	},
}
