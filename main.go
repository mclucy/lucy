// TODO: REPLACE ALL io.ReadAll WITH STREAMING METHODS

package main

import (
	"context"
	"os"

	"github.com/mclucy/lucy/cmd"
	"github.com/mclucy/lucy/logger"
)

func main() {
	defer logger.DumpHistory() // Whether DumpHistory actually does anything depend on the flag.
	if err := cmd.Cli.Run(context.Background(), os.Args); err != nil {
		logger.ReportError(err)
	}
}
