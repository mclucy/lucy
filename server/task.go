package server

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"slices"
	"sort"
	"strings"
)

const (
	TaskAdd     = "add"
	TaskInstall = "install"
	TaskRemove  = "remove"
)

// RunPackageTask starts a Lucy subprocess in the instance root under its run
// user, honoring context cancellation while retaining separate output streams.
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
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	result := PackageTaskResult{Output: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		if result.Stderr != "" {
			return result, fmt.Errorf("package task %s failed: %w\n%s", task.Name, err, result.Stderr)
		}
		return result, fmt.Errorf("package task %s failed: %w", task.Name, err)
	}
	return result, nil
}

// packageTaskArgs translates supported task options to the hidden run-task CLI,
// separating package arguments from flags and rejecting unknown tasks.
func packageTaskArgs(instance string, task PackageTaskRequest) ([]string, error) {
	args := []string{"run-task", instance, task.Name}
	allowed := PackageTaskFlagNames(task.Name)
	keys := make([]string, 0, len(task.Options))
	for key := range task.Options {
		if !slices.Contains(allowed, key) {
			return nil, fmt.Errorf("unsupported %s option %q", task.Name, key)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--"+key+"="+task.Options[key])
	}
	switch task.Name {
	case TaskAdd:
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

// PackageTaskFlagNames bounds subprocess options to the public operation's
// supported flags; control/selection flags never re-enter daemon dispatch.
func PackageTaskFlagNames(task string) []string {
	common := []string{"json", "json-compact", "no-interactive", "no-color", "no-style", "locked", "offline"}
	switch task {
	case TaskAdd:
		return append(common, "ecosystem", "with-optional")
	case TaskInstall:
		return append(common, "with-optional", "force", "accept-broken", "overwrite", "java", "mcdr-venv")
	case TaskRemove:
		return append(common, "ecosystem")
	default:
		return nil
	}
}
