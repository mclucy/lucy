package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// CommandTarget identifies the server directory a command operates on,
// either explicitly via --server or implicitly from the working directory.
type CommandTarget struct {
	WorkDir    string
	Instance   *Instance
	Registered bool
}

// ResolveCommandTarget determines which server directory a command acts on.
func ResolveCommandTarget(cmd *cobra.Command, inv *Invocation) (CommandTarget, error) {
	selected, _ := cmd.Flags().GetString(FlagServer)
	if selected != "" {
		var inst *Instance
		var err error
		if ValidateInstanceName(selected) == nil {
			inst, err = ReadInstance(selected)
		}
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
		// The root command has already selected ordinary serverregistry entries.
	}

	wd := inv.Directory
	inst, err := FindInstanceForPath(wd)
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

// DispatchPackageTask forwards a registered instance operation and its options
// to the daemon, then displays the child process's already formatted output.
func DispatchPackageTask(
	cmd *cobra.Command,
	inv *Invocation,
	target CommandTarget,
	task PackageTaskRequest,
) error {
	if target.Instance == nil {
		return fmt.Errorf("registered server target is missing instance data")
	}
	task.Options = make(map[string]string)
	for _, name := range PackageTaskFlagNames(task.Name) {
		flag := cmd.Flags().Lookup(name)
		if flag != nil && flag.Changed {
			task.Options[name] = flag.Value.String()
		}
	}
	// Preserve implicit agent defaults as well as explicitly supplied flags.
	for _, name := range []string{FlagJSON, FlagJSONCompact, FlagNoInteractive, FlagNoColor, FlagNoStyle} {
		if value, err := cmd.Flags().GetBool(name); err == nil && value {
			task.Options[name] = "true"
		}
	}
	var result PackageTaskResult
	if err := CallDaemonWithAutoStart(
		cmd.Context(),
		Request{
			Op:       OpPackageTask,
			Instance: target.Instance.Name,
			Task:     task,
		},
		&result,
		inv.Logger,
	); err != nil {
		return err
	}
	if result.Output != "" {
		if _, err := fmt.Fprint(cmd.OutOrStdout(), result.Output); err != nil {
			return err
		}
	}
	if result.Stderr != "" {
		_, err := fmt.Fprint(cmd.ErrOrStderr(), result.Stderr)
		return err
	}
	return nil
}
