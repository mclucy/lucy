package cli

import (
	"fmt"
	"os"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/cli/help"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/types"
	"github.com/spf13/cobra"
)

const (
	flagWithOptionalName = "with-optional"
	flagLockedName       = "locked"
	flagOfflineName      = "offline"
)

var addCmd = &cobra.Command{
	Use:   "add",
	Short: help.AddShort,
	Args:  cobra.MinimumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		ecosystem, _ := cmd.Flags().GetString(FlagEcosystem)
		if ecosystem != "" && !types.Ecosystem(ecosystem).Valid() {
			return fmt.Errorf("invalid ecosystem %q", ecosystem)
		}
		return nil
	},
	RunE: actionAdd,
}

func NewAddCommand() *cobra.Command {
	addCmd.Flags().Bool(
		flagWithOptionalName,
		false,
		"Also install optional upstream dependencies",
	)
	addCmd.Flags().Bool(
		flagLockedName,
		false,
		"Do not resolve new versions; fail when the lock cannot satisfy",
	)
	addCmd.Flags().Bool(
		flagOfflineName,
		false,
		"Disallow network access; fail if a needed file is not already available locally",
	)
	AddEcosystemFlag(addCmd)
	return addCmd
}

func actionAdd(cmd *cobra.Command, args []string) error {
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine working directory: %w", err)
	}

	withOptional, _ := cmd.Flags().GetBool(flagWithOptionalName)
	locked, _ := cmd.Flags().GetBool(flagLockedName)
	offline, _ := cmd.Flags().GetBool(flagOfflineName)

	ecosystem, _ := cmd.Flags().GetString(FlagEcosystem)

	opts := install.Options{
		Locked:          locked,
		Offline:         offline,
		WithOptional:    withOptional,
		PublishManifest: false,
	}

	diagnostics, warnings, err := operation.Add(cmd.Context(), workDir, args, types.Ecosystem(ecosystem), opts)
	for _, diagnostic := range diagnostics {
		report.Warn(fmt.Errorf("%s: %s", diagnostic.Path, diagnostic.Message))
	}
	for _, warning := range warnings {
		report.Warn(warning)
	}
	return err
}
