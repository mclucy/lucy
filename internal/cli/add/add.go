package add

import (
	"fmt"
	"os"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/types"
	"github.com/spf13/cobra"
)

const (
	flagForceName        = "force"
	flagWithOptionalName = "with-optional"
	flagNoOptionalName   = "no-optional"
	flagLockedName       = "locked"
	flagOfflineName      = "offline"
	flagLoaderName       = "loader"
	flagPlatformName     = cli.FlagPlatform
)

var addCmd = &cobra.Command{
	Use:   "add",
	Short: "Add packages under explicit operator control",
	Args:  cobra.MinimumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		withOptional, _ := cmd.Flags().GetBool(flagWithOptionalName)
		noOptional, _ := cmd.Flags().GetBool(flagNoOptionalName)
		if withOptional && noOptional {
			return fmt.Errorf("--with-optional and --no-optional cannot be used together")
		}
		loaderArg := resolveLoaderFlag(cmd)
		if loaderArg != "" && !types.Ecosystem(loaderArg).Valid() {
			return fmt.Errorf("invalid loader or platform %q", loaderArg)
		}
		return nil
	},
	RunE: cli.WithErrorLogging(actionAdd),
}

func NewCommand() *cobra.Command {
	addCmd.Flags().BoolP(
		flagForceName,
		"f",
		false,
		"Ignore version, dependency, and platform warnings",
	)
	addCmd.Flags().Bool(
		flagWithOptionalName,
		false,
		"Also install optional upstream dependencies",
	)
	addCmd.Flags().Bool(
		flagNoOptionalName,
		false,
		"Skip optional upstream dependencies (default)",
	)
	addCmd.Flags().Bool(
		flagLockedName,
		false,
		"Resolve only within what the lock already pins",
	)
	addCmd.Flags().Bool(
		flagOfflineName,
		false,
		"Disallow network acquisition for newly added files",
	)
	addCmd.Flags().StringP(
		flagLoaderName,
		"l",
		"",
		"Target loader scope for the request (fabric, forge, neoforge, bukkit)",
	)
	cli.AddPlatformFlag(addCmd)
	cli.AddNoStyleFlag(addCmd)
	return addCmd
}

func resolveLoaderFlag(cmd *cobra.Command) string {
	loader, _ := cmd.Flags().GetString(flagLoaderName)
	if loader != "" {
		return loader
	}
	platform, _ := cmd.Flags().GetString(flagPlatformName)
	return platform
}

func actionAdd(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}

	withOptional, _ := cmd.Flags().GetBool(flagWithOptionalName)
	locked, _ := cmd.Flags().GetBool(flagLockedName)
	offline, _ := cmd.Flags().GetBool(flagOfflineName)

	loaderArg := resolveLoaderFlag(cmd)
	loader := types.Ecosystem(loaderArg)

	opts := install.Options{
		Locked:          locked,
		Offline:         offline,
		WithOptional:    withOptional,
		PublishManifest: false,
	}

	return install.Add(cmd.Context(), workDir, args, loader, opts)
}
