package cli

import (
	"fmt"
	"os"
	"path/filepath"

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
		return runInTargetWorkDirUnlocked(target, fn)
	}
	if target.Registered && target.Instance != nil {
		return server.WithInstanceLock(target.Instance.Name, run)
	}
	return run()
}

func runInTargetWorkDirUnlocked(target CommandTarget, fn func() error) error {
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
	st := server.NewServiceManager().StatusInstance(*target.Instance)
	if st.Running {
		_ = server.MarkPendingRestart(target.Instance.Name, true, reason)
	}
}
