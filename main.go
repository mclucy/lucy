// TODO: REPLACE ALL io.ReadAll WITH STREAMING METHODS

package main

import (
	"os"

	"github.com/mclucy/lucy/cmd"
	"github.com/mclucy/lucy/logger"
)

func main() {
	defer logger.DumpHistory() // Whether DumpHistory actually does anything depends on the flag.
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
