package main

import (
	"os"

	"github.com/mclucy/lucy/cmd"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/report"
)

func main() {
	os.Exit(run())
}

func run() (exitCode int) {
	defer func() {
		if err := log.Close(); err != nil {
			report.Error(err)
			exitCode = 1
		}
	}()
	if err := cmd.Execute(); err != nil {
		return 1
	}
	return 0
}
