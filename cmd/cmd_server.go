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

	"al.essio.dev/pkg/shellescape"
	"charm.land/huh/v2"
	"github.com/google/shlex"
	"github.com/mclucy/lucy/internal/cli"
	"github.com/spf13/cobra"
)

const (
	flagServerRunUserName = "run-user"
	flagServerFollowName  = "follow"
	flagServerRawName     = "raw"
	flagServerBinaryName  = "binary"
)

// instanceRegistrar is the pair of native capabilities needed by server add.
type instanceRegistrar interface {
	cli.InstanceInstaller
	cli.InstanceEnabler
}

// newManagedServerCommand builds native lifecycle actions for one invocation.
// Its children are attached to the workspace registry's server command.
func newManagedServerCommand(inv *cli.Invocation) *cobra.Command {
	serverCmd := &cobra.Command{
		Use:   "server",
		Short: "Manage registered Minecraft server instances",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	serverAddCmd := &cobra.Command{
		Use:   "add <name> <path>",
		Short: "Register a Minecraft server instance",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionServerAdd(cmd, inv, args)
		},
	}

	serverListCmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List registered server instances",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionServerList(cmd, inv, args)
		},
	}

	serverStatusCmd := &cobra.Command{
		Use:   "status <name>",
		Short: "Show a registered server instance status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionServerStatus(cmd, inv, args)
		},
	}

	serverStartCmd := &cobra.Command{
		Use:   "start <name>",
		Short: "Start a registered server instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.CallDaemonWithAutoStart(cmd.Context(), cli.Request{Op: cli.OpStart, Instance: args[0]}, nil, inv.Logger)
		},
	}

	serverStopCmd := &cobra.Command{
		Use:   "stop <name>",
		Short: "Gracefully stop a registered server instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.CallDaemonWithAutoStart(cmd.Context(), cli.Request{Op: cli.OpStop, Instance: args[0]}, nil, inv.Logger)
		},
	}

	serverRestartCmd := &cobra.Command{
		Use:   "restart <name>",
		Short: "Restart a registered server instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.CallDaemonWithAutoStart(cmd.Context(), cli.Request{Op: cli.OpRestart, Instance: args[0]}, nil, inv.Logger)
		},
	}

	serverEnableCmd := &cobra.Command{
		Use:   "enable <name>",
		Short: "Enable a server instance at boot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.CallDaemonWithAutoStart(cmd.Context(), cli.Request{Op: cli.OpEnable, Instance: args[0]}, nil, inv.Logger)
		},
	}

	serverDisableCmd := &cobra.Command{
		Use:   "disable <name>",
		Short: "Disable a server instance at boot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli.CallDaemonWithAutoStart(cmd.Context(), cli.Request{Op: cli.OpDisable, Instance: args[0]}, nil, inv.Logger)
		},
	}

	serverSendCmd := &cobra.Command{
		Use:   "send <name> <command>",
		Short: "Send one command to a server console",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionServerSend(cmd, inv, args)
		},
	}

	serverLogsCmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Show a server console log",
		Args:  cobra.ExactArgs(1),
		RunE:  actionServerLogs,
	}

	serverAttachCmd := &cobra.Command{
		Use:   "attach <name>",
		Short: "Attach to a server console",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionServerAttach(cmd, inv, args)
		},
	}

	serverEditCmd := &cobra.Command{
		Use:   "edit <name>",
		Short: "Edit a server runtime configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionServerEdit(cmd, inv, args)
		},
	}

	serverRemoveCmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Unregister a server instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return actionServerRemove(cmd, inv, args)
		},
	}

	// Register runtime-user and configuration-editing flags.
	serverAddCmd.Flags().String(
		flagServerRunUserName,
		cli.DefaultRunUser,
		"System user that runs the Minecraft server",
	)
	serverAddCmd.Flags().String(
		flagServerBinaryName,
		"",
		"Path to the Lucy binary used by generated services",
	)
	serverLogsCmd.Flags().BoolP(flagServerFollowName, "f", false, "Follow log output")
	serverEditCmd.Flags().Bool(flagServerRawName, false, "Open lucy-cli.yaml in $EDITOR")

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
	return serverCmd
}

// actionServerAdd writes the instance registry, creates launch settings if absent,
// and installs and enables its native service without starting the cli.
func actionServerAdd(cmd *cobra.Command, inv *cli.Invocation, args []string) error {
	name := args[0]
	root := args[1]
	if !filepath.IsAbs(root) {
		root = filepath.Join(inv.Directory, root)
	}
	runUser, _ := cmd.Flags().GetString(flagServerRunUserName)
	binary, _ := cmd.Flags().GetString(flagServerBinaryName)
	if binary == "" {
		binary = cli.CurrentBinary()
	}

	inst, err := cli.NewInstance(name, root, runUser)
	if err != nil {
		return err
	}
	cfg := cli.GuessRuntimeConfig(inst.Root)
	if _, err := os.Stat(inst.RuntimeConfig); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat runtime config: %w", err)
		}
		if err := cli.WriteRuntimeConfig(inst.RuntimeConfig, &cfg); err != nil {
			return err
		}
	}
	if err := cli.WriteInstance(&inst); err != nil {
		return err
	}
	manager := cli.NewServiceManager().(instanceRegistrar)
	if err := manager.InstallInstance(inst, binary); err != nil {
		return fmt.Errorf("install instance service: %w", err)
	}
	if err := manager.EnableInstance(inst); err != nil {
		return fmt.Errorf("enable instance service: %w", err)
	}
	output := cli.NewOperationOutput(cmd, inv)
	output.Info(fmt.Sprintf("registered server %q at %s", inst.Name, inst.Root))
	output.Info("service enabled for boot; it will not start until the next boot or `lucy server start`")
	return nil
}

// actionServerList prints registry entries as JSON or adds native service state
// to the human-readable listing, without requiring a daemon connection.
func actionServerList(cmd *cobra.Command, inv *cli.Invocation, _ []string) error {
	instances, err := cli.ListInstances()
	if err != nil {
		return err
	}
	switch cli.JSONMode(cmd) {
	case cli.JSONCompact:
		return (cli.JSONResult[[]cli.Instance]{}).Write(cli.NewQueryOutput(cmd, inv), instances)
	case cli.JSONPretty:
		return (cli.JSONResult[[]cli.Instance]{}).Write(cli.NewQueryOutput(cmd, inv), instances)
	}
	if len(instances) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no registered servers")
		return nil
	}
	manager := cli.NewServiceManager().(cli.InstanceStatusReader)
	for _, inst := range instances {
		st := manager.StatusInstance(inst)
		fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  enabled=%t running=%t\n", inst.Name, inst.Root, st.Enabled, st.Running)
	}
	return nil
}

// actionServerStatus queries the daemon and falls back to registry and native
// service data if unavailable, then renders JSON or a human-readable status.
func actionServerStatus(cmd *cobra.Command, inv *cli.Invocation, args []string) error {
	var status cli.InstanceStatus
	if err := cli.CallDaemonWithAutoStart(cmd.Context(), cli.Request{Op: cli.OpStatus, Instance: args[0]}, &status, inv.Logger); err != nil {
		if ctxErr := cmd.Context().Err(); ctxErr != nil {
			return ctxErr
		}
		inst, readErr := cli.ReadInstance(args[0])
		if readErr != nil || inst == nil {
			return err
		}
		rt, readErr := cli.NewRuntimeStateService().Read(inst.Name)
		if readErr != nil {
			return readErr
		}
		status = cli.InstanceStatus{
			Instance:       *inst,
			Service:        cli.NewServiceManager().(cli.InstanceStatusReader).StatusInstance(*inst),
			PendingRestart: rt.PendingRestart,
			PendingReason:  rt.Reason,
		}
	}
	switch cli.JSONMode(cmd) {
	case cli.JSONCompact:
		return (cli.JSONResult[cli.InstanceStatus]{}).Write(cli.NewQueryOutput(cmd, inv), status)
	case cli.JSONPretty:
		return (cli.JSONResult[cli.InstanceStatus]{}).Write(cli.NewQueryOutput(cmd, inv), status)
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Server: %s\nRoot: %s\nService: %s enabled=%t running=%t\nRunner: connected=%t pid=%d\n",
		status.Instance.Name, status.Instance.Root, status.Service.NativeID, status.Service.Enabled, status.Service.Running, status.Runner.Connected, status.Runner.Pid)
	if status.PendingRestart {
		fmt.Fprintf(out, "Pending restart: yes (%s)\n", status.PendingReason)
	} else {
		fmt.Fprintln(out, "Pending restart: no")
	}
	return nil
}

// actionServerSend joins the command arguments into one console line and sends
// it through the daemon, returning any connection or dispatch error.
func actionServerSend(cmd *cobra.Command, inv *cli.Invocation, args []string) error {
	line := strings.Join(args[1:], " ")
	return cli.CallDaemonWithAutoStart(cmd.Context(), cli.Request{
		Op:       cli.OpSend,
		Instance: args[0],
		Line:     line,
	}, nil, inv.Logger)
}

// actionServerLogs streams the registered instance's configured console log to
// stdout, optionally following new output until the command is cancelled.
func actionServerLogs(cmd *cobra.Command, args []string) error {
	inst, err := cli.ReadInstance(args[0])
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("server %q is not registered", args[0])
	}
	cfg, err := cli.ReadRuntimeConfig(inst.RuntimeConfig)
	if err != nil {
		return err
	}
	if cfg == nil {
		return fmt.Errorf("runtime config %s is missing; restore it before reading logs", inst.RuntimeConfig)
	}
	follow, _ := cmd.Flags().GetBool(flagServerFollowName)
	return cli.StreamLog(cmd.Context(), cfg.Logs.ConsolePath, follow, cmd.OutOrStdout())
}

// actionServerAttach follows console output while forwarding stdin through the
// daemon; /detach and EOF end the attachment without stopping the cli.
func actionServerAttach(cmd *cobra.Command, inv *cli.Invocation, args []string) error {
	inst, err := cli.ReadInstance(args[0])
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("server %q is not registered", args[0])
	}
	cfg, err := cli.ReadRuntimeConfig(inst.RuntimeConfig)
	if err != nil {
		return err
	}
	if cfg == nil {
		return fmt.Errorf("runtime config %s is missing; restore it before attaching", inst.RuntimeConfig)
	}
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	go func() {
		_ = cli.StreamLog(ctx, cfg.Logs.ConsolePath, true, cmd.OutOrStdout())
	}()
	cli.NewOperationOutput(cmd, inv).Info("attached; use /detach or Ctrl+D to detach without stopping the server")
	scanner := bufio.NewScanner(cmd.InOrStdin())
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "/detach" {
			return nil
		}
		if err := cli.CallDaemonWithAutoStart(cmd.Context(), cli.Request{
			Op:       cli.OpSend,
			Instance: args[0],
			Line:     line,
		}, nil, inv.Logger); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// actionServerEdit edits launch settings through a form or the configured text
// editor; it saves configuration without restarting the running instance.
func actionServerEdit(cmd *cobra.Command, inv *cli.Invocation, args []string) error {
	inst, err := cli.ReadInstance(args[0])
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
	if !cli.NewPromptGate(cmd, inv).Available() {
		return fmt.Errorf("interactive editing is unavailable; edit %s directly or use server edit --raw", inst.RuntimeConfig)
	}
	cfg, err := cli.ReadRuntimeConfig(inst.RuntimeConfig)
	if err != nil {
		return err
	}
	if cfg == nil {
		fallback := cli.GuessRuntimeConfig(inst.Root)
		cfg = &fallback
	}
	command := cfg.Command
	argsText := shellescape.QuoteCommand(cfg.Args)
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
	parsedArgs, err := shlex.Split(argsText)
	if err != nil {
		return fmt.Errorf("parse launch arguments: %w", err)
	}
	newMemory := strings.TrimSpace(maxMemory)
	if newMemory != cfg.Memory.Max {
		if err := updateMaxMemory(parsedArgs, newMemory); err != nil {
			return err
		}
	}
	cfg.Command = strings.TrimSpace(command)
	cfg.Args = parsedArgs
	cfg.Memory.Max = newMemory
	cfg.Stop.Timeout = strings.TrimSpace(stopTimeout)
	return cli.WriteRuntimeConfig(inst.RuntimeConfig, cfg)
}

// actionServerRemove removes the native service before deleting its registry
// entry, leaving the Minecraft files and runtime configuration in place.
func actionServerRemove(cmd *cobra.Command, inv *cli.Invocation, args []string) error {
	inst, err := cli.ReadInstance(args[0])
	if err != nil {
		return err
	}
	if inst == nil {
		return fmt.Errorf("server %q is not registered", args[0])
	}
	manager := cli.NewServiceManager().(cli.InstanceRemover)
	if err := manager.RemoveInstance(*inst); err != nil {
		return err
	}
	if err := cli.RemoveInstance(inst.Name); err != nil {
		return err
	}
	cli.NewOperationOutput(cmd, inv).Info(fmt.Sprintf("unregistered server %q", inst.Name))
	return nil
}

// openEditor runs the executable named by EDITOR, falling back to vi, with the
// current terminal streams and returns its exit error.
func openEditor(path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	words, err := shlex.Split(editor)
	if err != nil || len(words) == 0 {
		return fmt.Errorf("invalid EDITOR %q: expected a program and optional quoted arguments", editor)
	}
	cmd := exec.Command(words[0], append(words[1:], filepath.Clean(path))...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// updateMaxMemory changes only an existing JVM heap option; custom launchers
// without one fail explicitly instead of silently saving an unused setting.
func updateMaxMemory(args []string, value string) error {
	if len(value) < 2 {
		return fmt.Errorf("max memory must be a positive integer followed by K, M or G")
	}
	if !strings.ContainsRune("kKmMgG", rune(value[len(value)-1])) || value[0] == '0' {
		return fmt.Errorf("invalid max memory %q", value)
	}
	for _, c := range value[:len(value)-1] {
		if c < '0' || c > '9' {
			return fmt.Errorf("invalid max memory %q", value)
		}
	}
	updated := false
	for i, arg := range args {
		if strings.HasPrefix(arg, "-Xmx") {
			args[i] = "-Xmx" + value
			updated = true
		}
	}
	if updated {
		return nil
	}
	return fmt.Errorf("this launcher has no -Xmx argument; edit its launcher memory settings directly")
}
