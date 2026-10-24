package main

import (
	"context"
	"errors"
	"os"

	"github.com/mclucy/lucy/cmd"
	"github.com/mclucy/lucy/log"
)

func main() {
	os.Exit(run())
}

func run() (exitCode int) {
	executeErr := cmd.Execute()
	closeErr := log.Close()
	if executeErr == nil && closeErr == nil {
		executeErr = cmd.FlushOutput()
	}
	switch {
	case cmd.JSONSelected():
		// One document on stderr. A close failure joins the action failure
		// instead of silently disappearing or producing a second document.
		if err := errors.Join(executeErr, closeErr); err != nil {
			cmd.WriteError(err)
		}
	case closeErr != nil:
		cmd.WriteError(closeErr)
	}
	switch {
	case executeErr == nil && closeErr == nil:
		return 0
	case errors.Is(executeErr, context.Canceled):
		return 130
	default:
		return 1
	}
}
