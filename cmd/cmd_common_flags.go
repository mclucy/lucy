package cmd

import (
	"errors"

	"github.com/mclucy/lucy/types"
	"github.com/urfave/cli/v3"
)

const (
	flagJsonName    = "json"
	flagLongName    = "long"
	flagNoStyleName = "no-style"
	flagSourceName  = "source"
)

var flagJsonOutput = &cli.BoolFlag{
	Name:  flagJsonName,
	Usage: "Print raw JSON response",
	Value: false,
}

var flagLongOutput = &cli.BoolFlag{
	Name:    flagLongName,
	Usage:   "Show hidden or collapsed output",
	Value:   false,
	Aliases: []string{"l"},
}

var flagSource = &cli.StringFlag{
	Name:    flagSourceName,
	Aliases: []string{"s"},
	Usage:   "To fetch info from `SOURCE`",
	Value:   "",
	Validator: func(s string) error {
		if types.StringToSource(s) == types.UnknownSource {
			return errors.New("unknown source " + s)
		}
		return nil
	},
}

var flagNoStyle = &cli.BoolFlag{
	Name:  flagNoStyleName,
	Usage: "Disable colored and styled output",
	Value: false,
}
