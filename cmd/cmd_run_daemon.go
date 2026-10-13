package cmd

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/internal/cli/add"
	"github.com/mclucy/lucy/internal/cli/install"
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

var runTaskCmd = &cobra.Command{
	Use:    "run-task <server> <task> [args...]",
	Short:  "Run a Lucy server package task",
	Hidden: true,
	Args:   cobra.MinimumNArgs(2),
	RunE:   cli.WithErrorLogging(actionRunTask),
}

const (
	flagForceName        = "force"
	flagWithOptionalName = "with-optional"
	flagNoOptionalName   = "no-optional"
)

// init exposes hidden daemon, runner and package-task entry points used by native
// services and subprocesses, including add-task and platform flags.
func init() {
	runTaskCmd.Flags().BoolP(
		flagForceName,
		"f",
		false,
		"Ignore version, dependency, and platform warnings",
	)
	runTaskCmd.Flags().Bool(
		flagWithOptionalName,
		false,
		"Also install optional upstream dependencies",
	)
	runTaskCmd.Flags().Bool(
		flagNoOptionalName,
		false,
		"Skip optional upstream dependencies",
	)
	cli.AddPlatformFlag(runTaskCmd)
	rootCmd.AddCommand(runDaemonCmd, runServerCmd, runTaskCmd)
}

// actionRunTask dispatches a supported package operation in the registered root
// without acquiring another instance lock; the daemon holds the outer lock.
func actionRunTask(cmd *cobra.Command, args []string) error {
	inst, err := server.ReadInstance(args[0])
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("server %q is not registered", args[0])
	}
	target := cli.CommandTarget{
		WorkDir:    inst.Root,
		Instance:   inst,
		Registered: true,
	}
	task := args[1]
	taskArgs := args[2:]

	return cli.RunInTargetWorkDirUnlocked(target, func() error {
		switch task {
		case server.TaskAdd:
			return add.RunTask(cmd, taskArgs, target)
		case server.TaskInstall:
			if len(taskArgs) > 0 {
				return fmt.Errorf("install task does not accept arguments")
			}
			return install.RunTask(cmd, target)
		case server.TaskRemove:
			return actionRemoveAt(cmd, taskArgs, target)
		default:
			return fmt.Errorf("unknown package task %q", task)
		}
	})
}
