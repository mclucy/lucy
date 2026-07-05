package cmd

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/server"
	"github.com/spf13/cobra"
)

var runDaemonCmd = &cobra.Command{
	Use:    "run-daemon",
	Short:  "Run the Lucy daemon",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: cli.WithErrorLogging(func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(
			cmd.Context(),
			syscall.SIGINT,
			syscall.SIGTERM,
		)
		defer stop()
		return server.RunDaemon(ctx)
	}),
}

var runServerCmd = &cobra.Command{
	Use:    "run-server <name>",
	Short:  "Run a Lucy-managed Minecraft server instance",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: cli.WithErrorLogging(func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(
			context.Background(),
			syscall.SIGINT,
			syscall.SIGTERM,
		)
		defer stop()
		return server.RunServer(ctx, args[0])
	}),
}

func init() {
	rootCmd.AddCommand(runDaemonCmd, runServerCmd)
}
