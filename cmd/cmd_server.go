package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/terminal/style"
	"github.com/spf13/cobra"
)

const (
	flagServerRunUserName = "run-user"
	flagServerFollowName  = "follow"
	flagServerRawName     = "raw"
	flagServerBinaryName  = "binary"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Manage registered Minecraft server instances",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var serverAddCmd = &cobra.Command{
	Use:   "add <name> <path>",
	Short: "Register a Minecraft server instance",
	Args:  cobra.ExactArgs(2),
	RunE:  cli.WithErrorLogging(actionServerAdd),
}

var serverListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List registered server instances",
	Args:    cobra.NoArgs,
	RunE:    cli.WithErrorLogging(actionServerList),
}

var serverStatusCmd = &cobra.Command{
	Use:   "status <name>",
	Short: "Show a registered server instance status",
	Args:  cobra.ExactArgs(1),
	RunE:  cli.WithErrorLogging(actionServerStatus),
}

var serverStartCmd = &cobra.Command{
	Use:   "start <name>",
	Short: "Start a registered server instance",
	Args:  cobra.ExactArgs(1),
	RunE: cli.WithErrorLogging(func(cmd *cobra.Command, args []string) error {
		return cli.CallDaemonWithAutoStart(cmd.Context(), server.Request{Op: server.OpStart, Instance: args[0]}, nil)
	}),
}

var serverStopCmd = &cobra.Command{
	Use:   "stop <name>",
	Short: "Gracefully stop a registered server instance",
	Args:  cobra.ExactArgs(1),
	RunE: cli.WithErrorLogging(func(cmd *cobra.Command, args []string) error {
		return cli.CallDaemonWithAutoStart(cmd.Context(), server.Request{Op: server.OpStop, Instance: args[0]}, nil)
	}),
}

var serverRestartCmd = &cobra.Command{
	Use:   "restart <name>",
	Short: "Restart a registered server instance",
	Args:  cobra.ExactArgs(1),
	RunE: cli.WithErrorLogging(func(cmd *cobra.Command, args []string) error {
		return cli.CallDaemonWithAutoStart(cmd.Context(), server.Request{Op: server.OpRestart, Instance: args[0]}, nil)
	}),
}

var serverEnableCmd = &cobra.Command{
	Use:   "enable <name>",
	Short: "Enable a server instance at boot",
	Args:  cobra.ExactArgs(1),
	RunE: cli.WithErrorLogging(func(cmd *cobra.Command, args []string) error {
		return cli.CallDaemonWithAutoStart(cmd.Context(), server.Request{Op: server.OpEnable, Instance: args[0]}, nil)
	}),
}

var serverDisableCmd = &cobra.Command{
	Use:   "disable <name>",
	Short: "Disable a server instance at boot",
	Args:  cobra.ExactArgs(1),
	RunE: cli.WithErrorLogging(func(cmd *cobra.Command, args []string) error {
		return cli.CallDaemonWithAutoStart(cmd.Context(), server.Request{Op: server.OpDisable, Instance: args[0]}, nil)
	}),
}

var serverSendCmd = &cobra.Command{
	Use:   "send <name> <command>",
	Short: "Send one command to a server console",
	Args:  cobra.MinimumNArgs(2),
	RunE:  cli.WithErrorLogging(actionServerSend),
}

var serverLogsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "Show a server console log",
	Args:  cobra.ExactArgs(1),
	RunE:  cli.WithErrorLogging(actionServerLogs),
}

var serverAttachCmd = &cobra.Command{
	Use:   "attach <name>",
	Short: "Attach to a server console",
	Args:  cobra.ExactArgs(1),
	RunE:  cli.WithErrorLogging(actionServerAttach),
}

var serverEditCmd = &cobra.Command{
	Use:   "edit <name>",
	Short: "Edit a server runtime configuration",
	Args:  cobra.ExactArgs(1),
	RunE:  cli.WithErrorLogging(actionServerEdit),
}

var serverRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Unregister a server instance",
	Args:  cobra.ExactArgs(1),
	RunE:  cli.WithErrorLogging(actionServerRemove),
}

func init() {
	serverAddCmd.Flags().String(
		flagServerRunUserName,
		server.DefaultRunUser,
		"System user that runs the Minecraft server",
	)
	serverAddCmd.Flags().String(
		flagServerBinaryName,
		"",
		"Path to the Lucy binary used by generated services",
	)
	serverLogsCmd.Flags().BoolP(flagServerFollowName, "f", false, "Follow log output")
	serverEditCmd.Flags().Bool(flagServerRawName, false, "Open lucy-server.yaml in $EDITOR")

	cli.AddJSONFlag(serverListCmd)
	cli.AddJSONFlag(serverStatusCmd)
	cli.AddJSONCompactFlag(serverListCmd)
	cli.AddJSONCompactFlag(serverStatusCmd)

	serverCmd.AddCommand(
		serverAddCmd,
		serverListCmd,
		serverStatusCmd,
		serverStartCmd,
		serverStopCmd,
		serverRestartCmd,
		serverEnableCmd,
		serverDisableCmd,
		serverLogsCmd,
		serverSendCmd,
		serverAttachCmd,
		serverRemoveCmd,
		serverEditCmd,
	)
	rootCmd.AddCommand(serverCmd)
}

func actionServerAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	root := args[1]
	runUser, _ := cmd.Flags().GetString(flagServerRunUserName)
	binary, _ := cmd.Flags().GetString(flagServerBinaryName)
	if binary == "" {
		binary = server.CurrentBinary()
	}

	inst, err := server.NewInstance(name, root, runUser)
	if err != nil {
		return err
	}
	cfg := server.GuessRuntimeConfig(inst.Root)
	if _, err := os.Stat(inst.RuntimeConfig); errors.Is(err, os.ErrNotExist) {
		if err := server.WriteRuntimeConfig(inst.RuntimeConfig, &cfg); err != nil {
			return err
		}
	}
	if err := server.WriteInstance(&inst); err != nil {
		return err
	}
	manager := server.NewServiceManager()
	if err := manager.InstallInstance(inst, binary); err != nil {
		return fmt.Errorf("install instance service: %w", err)
	}
	if err := manager.EnableInstance(inst); err != nil {
		return fmt.Errorf("enable instance service: %w", err)
	}
	log.ShowInfo(fmt.Sprintf("registered server %q at %s", inst.Name, inst.Root))
	log.ShowInfo("service enabled for boot; it will not start until the next boot or `lucy server start`")
	return nil
}

func actionServerList(cmd *cobra.Command, _ []string) error {
	instances, err := server.ListInstances()
	if err != nil {
		return err
	}
	jsonOut, _ := cmd.Flags().GetBool(cli.FlagJSON)
	jsonCompact, _ := cmd.Flags().GetBool(cli.FlagJSONCompact)
	if jsonOut || jsonCompact {
		if jsonCompact {
			style.PrintAsJsonCompact(instances)
		} else {
			style.PrintAsJson(instances)
		}
		return nil
	}
	if len(instances) == 0 {
		log.ShowInfo("no registered servers")
		return nil
	}
	manager := server.NewServiceManager()
	for _, inst := range instances {
		st := manager.StatusInstance(inst)
		log.ShowInfo(fmt.Sprintf("%s  %s  enabled=%t running=%t", inst.Name, inst.Root, st.Enabled, st.Running))
	}
	return nil
}

func actionServerStatus(cmd *cobra.Command, args []string) error {
	var status server.InstanceStatus
	if err := cli.CallDaemonWithAutoStart(cmd.Context(), server.Request{Op: server.OpStatus, Instance: args[0]}, &status); err != nil {
		inst, readErr := server.ReadInstance(args[0])
		if readErr != nil || inst == nil {
			return err
		}
		rt, _ := server.ReadRuntimeState(inst.Name)
		status = server.InstanceStatus{
			Instance:       *inst,
			Service:        server.NewServiceManager().StatusInstance(*inst),
			PendingRestart: rt.PendingRestart,
			PendingReason:  rt.Reason,
		}
	}
	jsonOut, _ := cmd.Flags().GetBool(cli.FlagJSON)
	jsonCompact, _ := cmd.Flags().GetBool(cli.FlagJSONCompact)
	if jsonOut || jsonCompact {
		if jsonCompact {
			style.PrintAsJsonCompact(status)
		} else {
			style.PrintAsJson(status)
		}
		return nil
	}
	log.ShowInfo(fmt.Sprintf("Server: %s", status.Instance.Name))
	log.ShowInfo(fmt.Sprintf("Root: %s", status.Instance.Root))
	log.ShowInfo(fmt.Sprintf("Service: %s enabled=%t running=%t", status.Service.NativeID, status.Service.Enabled, status.Service.Running))
	log.ShowInfo(fmt.Sprintf("Runner: connected=%t pid=%d", status.Runner.Connected, status.Runner.Pid))
	if status.PendingRestart {
		log.ShowInfo(fmt.Sprintf("Pending restart: yes (%s)", status.PendingReason))
	} else {
		log.ShowInfo("Pending restart: no")
	}
	return nil
}

func actionServerSend(cmd *cobra.Command, args []string) error {
	line := strings.Join(args[1:], " ")
	return cli.CallDaemonWithAutoStart(cmd.Context(), server.Request{
		Op:       server.OpSend,
		Instance: args[0],
		Line:     line,
	}, nil)
}

func actionServerLogs(cmd *cobra.Command, args []string) error {
	inst, err := server.ReadInstance(args[0])
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("server %q is not registered", args[0])
	}
	cfg, err := server.ReadRuntimeConfig(inst.RuntimeConfig)
	if err != nil {
		return err
	}
	follow, _ := cmd.Flags().GetBool(flagServerFollowName)
	return server.StreamLog(cmd.Context(), cfg.Logs.ConsolePath, follow, os.Stdout)
}

func actionServerAttach(cmd *cobra.Command, args []string) error {
	inst, err := server.ReadInstance(args[0])
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("server %q is not registered", args[0])
	}
	cfg, err := server.ReadRuntimeConfig(inst.RuntimeConfig)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	go func() {
		_ = server.StreamLog(ctx, cfg.Logs.ConsolePath, true, os.Stdout)
	}()
	log.ShowInfo("attached; use /detach or Ctrl+D to detach without stopping the server")
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "/detach" {
			return nil
		}
		if err := cli.CallDaemonWithAutoStart(cmd.Context(), server.Request{
			Op:       server.OpSend,
			Instance: args[0],
			Line:     line,
		}, nil); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func actionServerEdit(cmd *cobra.Command, args []string) error {
	inst, err := server.ReadInstance(args[0])
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("server %q is not registered", args[0])
	}
	raw, _ := cmd.Flags().GetBool(flagServerRawName)
	if raw {
		return openEditor(inst.RuntimeConfig)
	}
	cfg, err := server.ReadRuntimeConfig(inst.RuntimeConfig)
	if err != nil {
		return err
	}
	command := cfg.Command
	argsText := strings.Join(cfg.Args, " ")
	maxMemory := cfg.Memory.Max
	stopTimeout := cfg.Stop.Timeout
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Command").Value(&command),
			huh.NewInput().Title("Arguments").Value(&argsText),
			huh.NewInput().Title("Max memory").Value(&maxMemory),
			huh.NewInput().Title("Stop timeout").Value(&stopTimeout),
		),
	)
	if err := form.Run(); err != nil {
		return err
	}
	cfg.Command = strings.TrimSpace(command)
	cfg.Args = strings.Fields(argsText)
	cfg.Memory.Max = strings.TrimSpace(maxMemory)
	cfg.Stop.Timeout = strings.TrimSpace(stopTimeout)
	return server.WriteRuntimeConfig(inst.RuntimeConfig, cfg)
}

func actionServerRemove(_ *cobra.Command, args []string) error {
	inst, err := server.ReadInstance(args[0])
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("server %q is not registered", args[0])
	}
	manager := server.NewServiceManager()
	if err := manager.RemoveInstance(*inst); err != nil {
		return err
	}
	if err := server.RemoveInstance(inst.Name); err != nil {
		return err
	}
	log.ShowInfo(fmt.Sprintf("unregistered server %q", inst.Name))
	return nil
}

func openEditor(path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, filepath.Clean(path))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
