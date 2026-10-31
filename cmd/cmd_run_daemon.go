package cmd

import (
	"fmt"

	"github.com/mclucy/lucy/internal/cli"
	"github.com/spf13/cobra"
)

// newSupervisorCommands builds private native-service entry points per invocation.
func newSupervisorCommands(contracts *cli.JSONContracts, inv *cli.Invocation, deps *cli.Dependencies) []*cobra.Command {
	runDaemonCmd := &cobra.Command{
		Use:    "run-daemon",
		Short:  "Run the Lucy daemon",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunDaemon(cmd.Context())
		},
	}

	runServerCmd := &cobra.Command{
		Use:    "run-server <name>",
		Short:  "Run a Lucy-managed Minecraft server instance",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.RunServer(cmd.Context(), args[0])
		},
	}

	runTaskCmd := &cobra.Command{
		Use:    "run-task <server> <task> [args...]",
		Short:  "Run a Lucy server package task",
		Hidden: true,
		Args:   cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionRunTask(cmd, inv, deps, args)
		},
	}

	cli.RegisterPackageTaskFlags(runTaskCmd, contracts)
	return []*cobra.Command{runDaemonCmd, runServerCmd, runTaskCmd}
}

// actionRunTask dispatches a supported package operation in the registered root
// without acquiring another instance lock; the daemon holds the outer lock.
func actionRunTask(cmd *cobra.Command, inv *cli.Invocation, deps *cli.Dependencies, args []string) error {
	task := args[1]
	taskArgs := args[2:]

	// ResolveServer has selected the root before dependencies were assembled.
	// The daemon owns the outer lock and restart marker; the child only runs
	// the ordinary operation and its output contract.
	switch task {
	case cli.TaskAdd:
		if len(taskArgs) == 0 {
			return fmt.Errorf("add task needs at least one package")
		}
		return cli.RunAddTask(cmd, inv, deps, taskArgs)
	case cli.TaskInstall:
		if len(taskArgs) > 0 {
			return fmt.Errorf("install task does not accept arguments")
		}
		return cli.RunInstallTask(cmd, inv, deps)
	case cli.TaskRemove:
		if len(taskArgs) == 0 {
			return fmt.Errorf("remove task needs at least one package")
		}
		return cli.RunRemoveTask(cmd, inv, deps, taskArgs)
	default:
		return fmt.Errorf("unknown package task %q", task)
	}
}
