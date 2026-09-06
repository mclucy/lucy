package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/workspace"
	"github.com/spf13/cobra"
)

// CommandTarget identifies the server directory a command operates on,
// either explicitly via --server or implicitly from the working directory.
type CommandTarget struct {
	WorkDir    string
	Instance   *server.Instance
	Registered bool
}

// ResolveCommandTarget determines which server directory a command acts on.
func ResolveCommandTarget(cmd *cobra.Command) (CommandTarget, error) {
	selected, _ := cmd.Flags().GetString(FlagServer)
	if selected != "" {
		inst, err := server.ReadInstance(selected)
		if err != nil {
			return CommandTarget{}, err
		}
		if inst == nil {
			return CommandTarget{}, fmt.Errorf("server %q is not registered", selected)
		}
		return CommandTarget{
			WorkDir:    inst.Root,
			Instance:   inst,
			Registered: true,
		}, nil
	}

	wd, err := os.Getwd()
	if err != nil {
		return CommandTarget{}, fmt.Errorf("could not determine working directory: %w", err)
	}
	inst, err := server.FindInstanceForPath(wd)
	if err != nil {
		return CommandTarget{}, err
	}
	if inst != nil {
		return CommandTarget{
			WorkDir:    inst.Root,
			Instance:   inst,
			Registered: true,
		}, nil
	}
	return CommandTarget{WorkDir: wd}, nil
}

// RunInTargetWorkDir runs fn inside the target's server directory, taking the
// instance lock when the target is a registered Lucy server instance.
func RunInTargetWorkDir(target CommandTarget, fn func() error) error {
	run := func() error {
		return RunInTargetWorkDirUnlocked(target, fn)
	}
	if target.Registered && target.Instance != nil {
		return server.WithInstanceLock(target.Instance.Name, run)
	}
	return run()
}

// RunInTargetWorkDirUnlocked switches workspace context for a callback whose
// caller already owns the instance lock, restoring the original directory after it.
func RunInTargetWorkDirUnlocked(target CommandTarget, fn func() error) error {
	current, err := os.Getwd()
	if err != nil {
		return err
	}
	targetDir, err := filepath.Abs(target.WorkDir)
	if err != nil {
		return err
	}
	currentDir, err := filepath.Abs(current)
	if err != nil {
		return err
	}
	if targetDir == currentDir {
		workspace.Invalidate()
		return fn()
	}
	if err := os.Chdir(targetDir); err != nil {
		return fmt.Errorf("enter server directory %s: %w", targetDir, err)
	}
	defer func() {
		_ = os.Chdir(current)
		workspace.Invalidate()
	}()
	workspace.Invalidate()
	return fn()
}

// MarkPendingRestartIfRunning flags a running managed server for a restart so
// it picks up package files changed by this command.
func MarkPendingRestartIfRunning(target CommandTarget, reason string) {
	if !target.Registered || target.Instance == nil {
		return
	}
	st := server.NewServiceManager().(server.InstanceStatusReader).StatusInstance(*target.Instance)
	if st.Running {
		if err := server.NewRuntimeStateService().MarkPendingRestart(target.Instance.Name, true, reason); err != nil {
			log.ShowWarn(fmt.Errorf("package changes completed, but could not save restart status: %w; restart server %q manually", err, target.Instance.Name))
		}
	}
}

// DispatchPackageTask forwards a registered instance operation and its options
// to the daemon, then displays the child process's already formatted output.
func DispatchPackageTask(
	cmd *cobra.Command,
	target CommandTarget,
	task server.PackageTaskRequest,
) error {
	if target.Instance == nil {
		return fmt.Errorf("registered server target is missing instance data")
	}
	task.Platform, _ = cmd.Flags().GetString(FlagPlatform)
	task.UseGitHubMirror, _ = cmd.Flags().GetBool(FlagUseGitHubMirror)
	var result server.PackageTaskResult
	if err := CallDaemonWithAutoStart(
		cmd.Context(),
		server.Request{
			Op:       server.OpPackageTask,
			Instance: target.Instance.Name,
			Task:     task,
		},
		&result,
	); err != nil {
		return err
	}
	if result.Output != "" {
		return log.ShowRaw(result.Output)
	}
	return nil
}

// MustBoolFlag reads a boolean flag, defaulting to false when unset.
func MustBoolFlag(cmd *cobra.Command, name string) bool {
	value, _ := cmd.Flags().GetBool(name)
	return value
}
