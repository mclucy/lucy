package cmd

import (
	"context"

	"github.com/urfave/cli/v3"
)

var subcmdConfig = &cli.Command{
	Name:   "config",
	Usage:  "Manage lucy's configurations",
	Action: actionEmpty, // tools.Decorate(actionInit, decorator),
}

var actionConfig cli.ActionFunc = func(
	ctx context.Context,
	cmd *cli.Command,
) error {
	return nil
}
