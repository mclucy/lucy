package server

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const (
	TaskAdd     = "add"
	TaskInstall = "install"
	TaskRemove  = "remove"
)

func RunPackageTask(
	ctx context.Context,
	inst Instance,
	task PackageTaskRequest,
) (PackageTaskResult, error) {
	if task.Name == "" {
		return PackageTaskResult{}, fmt.Errorf("package task name is required")
	}

	args, err := packageTaskArgs(inst.Name, task)
	if err != nil {
		return PackageTaskResult{}, err
	}
	cmd := exec.CommandContext(ctx, CurrentBinary(), args...)
	cmd.Dir = inst.Root
	if err := applyRunUser(cmd, inst.RunUser); err != nil {
		return PackageTaskResult{}, err
	}
	out, err := cmd.CombinedOutput()
	result := PackageTaskResult{Output: string(out)}
	if err != nil {
		if result.Output != "" {
			return result, fmt.Errorf("package task %s failed: %w\n%s", task.Name, err, result.Output)
		}
		return result, fmt.Errorf("package task %s failed: %w", task.Name, err)
	}
	return result, nil
}

func packageTaskArgs(instance string, task PackageTaskRequest) ([]string, error) {
	args := []string{"run-task", instance, task.Name}
	if task.Platform != "" {
		args = append(args, "--platform", task.Platform)
	}
	if task.UseGitHubMirror {
		args = append(args, "--use-github-mirror")
	}
	switch task.Name {
	case TaskAdd:
		if task.AddOptions.Force {
			args = append(args, "--force")
		}
		if task.AddOptions.WithOptional {
			args = append(args, "--with-optional")
		}
		if task.AddOptions.NoOptional {
			args = append(args, "--no-optional")
		}
		args = append(args, "--")
		args = append(args, task.Args...)
	case TaskInstall:
		if len(task.Args) > 0 {
			return nil, fmt.Errorf("install task does not accept arguments: %s", strings.Join(task.Args, " "))
		}
	case TaskRemove:
		args = append(args, "--")
		args = append(args, task.Args...)
	default:
		return nil, fmt.Errorf("unknown package task %q", task.Name)
	}
	return args, nil
}
