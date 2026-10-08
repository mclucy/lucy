package cmd

import (
	"fmt"
	"os"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/types"
	"github.com/spf13/cobra"
)

const (
	flagRemoveLockedName  = "locked"
	flagRemoveOfflineName = "offline"
	flagRemoveLoaderName  = "loader"
)

var removeCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove packages under explicit operator control",
	Args:  cobra.MinimumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		loaderArg := resolveRemoveLoaderFlag(cmd)
		if loaderArg != "" && !types.Ecosystem(loaderArg).Valid() {
			return fmt.Errorf("invalid loader or platform %q", loaderArg)
		}
		return nil
	},
	RunE: cli.WithErrorLogging(actionRemove),
}

func init() {
	removeCmd.Flags().Bool(
		flagRemoveLockedName,
		false,
		"Re-resolve using only what the lock already pins",
	)
	removeCmd.Flags().Bool(
		flagRemoveOfflineName,
		false,
		"Disallow network acquisition for any re-resolution this triggers",
	)
	removeCmd.Flags().StringP(
		flagRemoveLoaderName,
		"l",
		"",
		"Target loader scope for the request (fabric, forge, neoforge, bukkit)",
	)
	cli.AddNoStyleFlag(removeCmd)
	cli.AddPlatformFlag(removeCmd)
	rootCmd.AddCommand(removeCmd)
}

func resolveRemoveLoaderFlag(cmd *cobra.Command) string {
	loader, _ := cmd.Flags().GetString(flagRemoveLoaderName)
	if loader != "" {
		return loader
	}
	platform, _ := cmd.Flags().GetString(cli.FlagPlatform)
	return platform
}

func actionRemove(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}

	locked, _ := cmd.Flags().GetBool(flagRemoveLockedName)
	offline, _ := cmd.Flags().GetBool(flagRemoveOfflineName)

	loaderArg := resolveRemoveLoaderFlag(cmd)
	loader := types.Ecosystem(loaderArg)

	opts := install.Options{
		Locked:          locked,
		Offline:         offline,
		WithOptional:    false,
		PublishManifest: false,
	}

	return install.Remove(cmd.Context(), workDir, args, loader, opts)
}
