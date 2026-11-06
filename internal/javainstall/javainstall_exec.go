package javainstall

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/internal/shell"
)

// errChildUnconfirmed reports that a step process ended from a signal other
// than the SIGINT expected after a relayed cancellation. Such a process could
// not have waited for the child it started, so a later restore must not
// assume that child has stopped.
var errChildUnconfirmed = errors.New("process exited from an unexpected signal; a child it started may still be running")

// Execute runs plan, using input for interactive steps and out for output.
// It returns the installed Java home for a managed download.
func (h *Host) Execute(
	ctx context.Context,
	plan Plan,
	policy Policy,
	input io.Reader,
	out io.Writer,
) (string, error) {
	h.logger.Info(
		"installing java",
		"method", plan.Method,
		"package", plan.Package,
		"major", plan.Major,
		"distribution", plan.Distribution.String(),
	)
	if plan.download != nil {
		if err := runDownload(ctx, h, plan.download, plan.managedRoot, out); err != nil {
			return "", err
		}
		h.logger.Info("java installed", "method", plan.Method, "package", plan.Package)
		home := filepath.Join(plan.managedRoot, plan.download.destination)
		if info, err := os.Stat(filepath.Join(home, "Contents", "Home")); err == nil && info.IsDir() {
			home = filepath.Join(home, "Contents", "Home")
		}
		return home, nil
	}
	if plan.Repository != nil {
		if err := addRepository(ctx, plan.Repository, policy, h, input, out); err != nil {
			return "", fmt.Errorf("add %s: %w", plan.Repository.Name, err)
		}
		h.logger.Info(
			"java repository added",
			"repository", plan.Repository.Name,
			"url", plan.Repository.URL,
		)
	}
	previous := ""
	if plan.KeepDefault {
		previous = systemDefaultJava(h)
	}
	var installErr error
	// A step that ended from an unexpected signal may still have a child
	// running; do not restore over it.
	restoreSafe := true
	run := runStep
	if plan.KeepDefault && previous != "" {
		run = runStepWaitingForStop
	}
	for _, step := range plan.Steps {
		if err := run(ctx, step, policy, h, input, out); err != nil {
			installErr = err
			restoreSafe = !errors.Is(err, errChildUnconfirmed)
			break
		}
	}
	if plan.KeepDefault && previous != "" && len(plan.Steps) > 0 {
		if !restoreSafe {
			installErr = errors.Join(installErr, fmt.Errorf(
				"system default Java %s was not restored because an install process exited from an unexpected signal and its child could not be confirmed stopped",
				previous,
			))
		} else if err := restoreSystemDefault(ctx, previous, policy, h, input, out); err != nil {
			restoreErr := fmt.Errorf("restore the system default Java %s: %w", previous, err)
			if installErr == nil {
				installErr = restoreErr
			} else {
				installErr = errors.Join(installErr, restoreErr)
			}
		}
	}
	if installErr != nil {
		return "", installErr
	}
	h.logger.Info("java installed", "method", plan.Method, "package", plan.Package)
	return "", nil
}

// Commands describes what Execute runs, one shell-like line per action, in
// the order Execute runs them.
func (p Plan) Commands() []string {
	if p.download != nil {
		return p.download.describe(p.managedRoot)
	}
	var lines []string
	if r := p.Repository; r != nil {
		if r.Key != nil {
			lines = append(
				lines,
				fmt.Sprintf("download %s and verify fingerprint %s", r.Key.URL, r.Key.Fingerprint),
				rootPrefix(p.root)+"write "+r.Key.Path,
			)
		}
		for _, file := range r.Files {
			lines = append(lines, rootPrefix(p.root)+"write "+file.Path)
		}
		for _, step := range r.Steps {
			lines = append(lines, step.command(p.root))
		}
	}
	for _, step := range p.Steps {
		lines = append(lines, step.command(p.root))
	}
	return lines
}

// command renders the step as a command line a user can run. root reports
// whether the plan was made as root.
func (s Step) command(root bool) string {
	args := s.Args
	if s.Elevation == ElevationRoot && !root {
		args = append([]string{"sudo"}, args...)
	}
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shell.Quote(arg)
	}
	return strings.Join(quoted, " ")
}

func rootPrefix(root bool) string {
	if root {
		return ""
	}
	return "sudo "
}

// runStep runs one step with the ordinary cancellation behavior: a canceled
// context kills the process.
func runStep(
	ctx context.Context,
	step Step,
	policy Policy,
	host *Host,
	input io.Reader,
	out io.Writer,
) error {
	return runStepWithCancel(ctx, step, policy, host, input, out, false)
}

// runStepWaitingForStop runs a step whose completion gates a restore. A
// canceled context interrupts the process and waits for it to exit, so the
// restore cannot race an elevated child.
func runStepWaitingForStop(
	ctx context.Context,
	step Step,
	policy Policy,
	host *Host,
	input io.Reader,
	out io.Writer,
) error {
	return runStepWithCancel(ctx, step, policy, host, input, out, true)
}

func runStepWithCancel(
	ctx context.Context,
	step Step,
	policy Policy,
	host *Host,
	input io.Reader,
	out io.Writer,
	interruptOnCancel bool,
) error {
	if len(step.Args) == 0 {
		return errors.New("empty install step")
	}
	args := append([]string(nil), step.Args...)
	path, err := host.lookPath(args[0])
	if err != nil {
		return fmt.Errorf("%s: %w", step.command(host.isRoot()), err)
	}
	args[0] = path
	root := host.isRoot()
	if step.Elevation == ElevationRoot && !root {
		sudo, err := host.lookPath("sudo")
		if err != nil {
			return fmt.Errorf("%s: %w", step.command(root), err)
		}
		prefix := []string{sudo}
		if !policy.Interactive {
			prefix = append(prefix, "-n")
		}
		args = append(append(prefix, "--"), args...)
	}
	host.logger.Info("running java install step", "command", strings.Join(args, " "))
	stdin := input
	if step.Input != "" {
		stdin = strings.NewReader(step.Input)
	}
	if err := host.system.Run(ctx, Command{
		Args:              args,
		Env:               host.environ(),
		Stdin:             stdin,
		Stdout:            out,
		Stderr:            out,
		InterruptOnCancel: interruptOnCancel,
	}); err != nil {
		return fmt.Errorf("%s: %w", step.command(root), err)
	}
	return nil
}

// writeRootFile writes data at path as root. A non-root process stages the
// bytes in a temporary file and copies them with install(1) through sudo.
func writeRootFile(
	ctx context.Context,
	path string,
	data []byte,
	policy Policy,
	host *Host,
	input io.Reader,
	out io.Writer,
) error {
	if host.isRoot() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, data, 0o644)
	}
	staged, err := os.CreateTemp("", "lucy-java-*")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	if _, err := staged.Write(data); err != nil {
		return errors.Join(err, staged.Close())
	}
	if err := staged.Close(); err != nil {
		return err
	}
	return runStep(ctx, Step{
		Args:      []string{"install", "-D", "-m", "0644", staged.Name(), path},
		Elevation: ElevationRoot,
	}, policy, host, input, out)
}
