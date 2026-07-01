package main

import (
	"os"

	"github.com/mclucy/lucy/cmd"
	"github.com/mclucy/lucy/log"
)

func main() {
	defer log.DumpHistory() // Whether DumpHistory actually does anything depends on the flag.
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
