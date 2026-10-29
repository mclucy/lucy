package cmd

import (
	"fmt"

	"github.com/mclucy/lucy/report"
	"github.com/mclucy/lucy/server"
	"github.com/spf13/cobra"
)

const flagInitDaemonBinaryName = "binary"

// newDaemonCommands creates independent daemon installation entry points.
func newDaemonCommands() []*cobra.Command {
	var initDaemonCmd = &cobra.Command{
		Use:   "init-daemon",
		Short: "Install and start the Lucy daemon service",
		Args:  cobra.NoArgs,
		RunE:  actionInitDaemon,
	}

	var daemonCmd = &cobra.Command{
		Use:    "daemon",
		Short:  "Manage the Lucy daemon",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	var daemonInitCmd = &cobra.Command{
		Use:    "init",
		Short:  "Install and start the Lucy daemon service",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE:   actionInitDaemon,
	}

	// init registers init-daemon and its hidden daemon init alias with an optional
	// binary path for native service installation.
	initDaemonCmd.Flags().String(
		flagInitDaemonBinaryName,
		"",
		"Path to the Lucy binary used by generated services",
	)
	daemonInitCmd.Flags().String(
		flagInitDaemonBinaryName,
		"",
		"Path to the Lucy binary used by generated services",
	)
	daemonCmd.AddCommand(daemonInitCmd)
	return []*cobra.Command{initDaemonCmd, daemonCmd}
}

// actionInitDaemon ensures the administration group and installs, enables and
// starts the daemon service, returning failures before reporting success.
func actionInitDaemon(cmd *cobra.Command, _ []string) error {
	binary, _ := cmd.Flags().GetString(flagInitDaemonBinaryName)
	if binary == "" {
		binary = server.CurrentBinary()
	}
	if err := server.EnsureAdminGroup(); err != nil {
		return err
	}
	if err := server.NewServiceManager().(server.DaemonInstaller).InstallDaemon(binary); err != nil {
		return fmt.Errorf("install Lucy daemon: %w", err)
	}
	report.Info("Lucy daemon service installed, enabled, and started")
	report.Info("No Minecraft server instances were started; add one with `lucy server add <name> <path>`")
	return nil
}
