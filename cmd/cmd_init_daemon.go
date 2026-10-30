package cmd

import (
	"fmt"

	"github.com/mclucy/lucy/internal/cli"
	"github.com/spf13/cobra"
)

const flagInitDaemonBinaryName = "binary"

// newDaemonCommands creates independent daemon installation entry points.
func newDaemonCommands(inv *cli.Invocation) []*cobra.Command {
	install := func(cmd *cobra.Command, args []string) error {
		return actionInitDaemon(cmd, inv, args)
	}
	initDaemonCmd := &cobra.Command{
		Use:   "init-daemon",
		Short: "Install and start the Lucy daemon service",
		Args:  cobra.NoArgs,
		RunE:  install,
	}

	daemonCmd := &cobra.Command{
		Use:    "daemon",
		Short:  "Manage the Lucy daemon",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	daemonInitCmd := &cobra.Command{
		Use:    "init",
		Short:  "Install and start the Lucy daemon service",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE:   install,
	}

	// Both entry points accept the binary used by native services.
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
func actionInitDaemon(cmd *cobra.Command, inv *cli.Invocation, _ []string) error {
	binary, _ := cmd.Flags().GetString(flagInitDaemonBinaryName)
	if binary == "" {
		binary = cli.CurrentBinary()
	}
	if err := cli.EnsureAdminGroup(); err != nil {
		return err
	}
	if err := cli.NewServiceManager().(cli.DaemonInstaller).InstallDaemon(binary); err != nil {
		return fmt.Errorf("install Lucy daemon: %w", err)
	}
	output := cli.NewOperationOutput(cmd, inv)
	output.Info("Lucy daemon service installed, enabled, and started")
	output.Info("No Minecraft server instances were started; add one with `lucy server add <name> <path>`")
	return nil
}
