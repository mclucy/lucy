package javainstall

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mclucy/lucy/internal/shell"
	"github.com/mclucy/lucy/log"
)

// Execute runs plan. Command output goes to out. A step without input reads
// the terminal, so a tool that asks for a password or an agreement can prompt.
func Execute(ctx context.Context, plan Plan, host Host, out io.Writer) error {
	log.Info(
		"installing java",
		"method", plan.Method,
		"package", plan.Package,
		"major", plan.Major,
		"distribution", plan.Distribution.String(),
	)
	if plan.download != nil {
		if err := runDownload(ctx, plan.download, out); err != nil {
			return err
		}
		log.Info("java installed", "method", plan.Method, "package", plan.Package)
		return nil
	}
	if plan.Repository != nil {
		if err := addRepository(ctx, plan.Repository, host, out); err != nil {
			return fmt.Errorf("add %s: %w", plan.Repository.Name, err)
		}
		log.Info("java repository added", "repository", plan.Repository.Name, "url", plan.Repository.URL)
	}
	previous := ""
	if plan.KeepDefault {
		previous = systemDefaultJava()
	}
	for _, step := range plan.Steps {
		if err := runStep(ctx, step, host, out); err != nil {
			return err
		}
	}
	if plan.KeepDefault && previous != "" {
		if err := restoreSystemDefault(ctx, previous, host, out); err != nil {
			return fmt.Errorf("restore the system default Java %s: %w", previous, err)
		}
	}
	log.Info("java installed", "method", plan.Method, "package", plan.Package)
	return nil
}

// Commands describes what Execute runs, one shell-like line per action, in
// the order Execute runs them.
func (p Plan) Commands() []string {
	if p.download != nil {
		return p.download.describe()
	}
	var lines []string
	if r := p.Repository; r != nil {
		if r.Key != nil {
			lines = append(
				lines,
				fmt.Sprintf("download %s and verify fingerprint %s", r.Key.URL, r.Key.Fingerprint),
				rootPrefix()+"write "+r.Key.Path,
			)
		}
		for _, file := range r.Files {
			lines = append(lines, rootPrefix()+"write "+file.Path)
		}
		for _, step := range r.Steps {
			lines = append(lines, step.String())
		}
	}
	for _, step := range p.Steps {
		lines = append(lines, step.String())
	}
	return lines
}

// String renders the step as a command line a user can run.
func (s Step) String() string {
	args := s.Args
	if s.Elevation == ElevationRoot && !isRoot() {
		args = append([]string{"sudo"}, args...)
	}
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shell.Quote(arg)
	}
	return strings.Join(quoted, " ")
}

// elevated returns the arguments that run the step as root from a non-root
// process. Without a terminal, sudo must fail instead of prompting.
func (s Step) elevated(interactive bool) []string {
	prefix := []string{"sudo"}
	if !interactive {
		prefix = append(prefix, "-n")
	}
	return append(append(prefix, "--"), s.Args...)
}

func rootPrefix() string {
	if isRoot() {
		return ""
	}
	return "sudo "
}

func runStep(ctx context.Context, step Step, host Host, out io.Writer) error {
	if len(step.Args) == 0 {
		return errors.New("empty install step")
	}
	args := step.Args
	if step.Elevation == ElevationRoot && !isRoot() {
		args = step.elevated(host.Interactive)
	}
	log.Info("running java install step", "command", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdout = out
	cmd.Stderr = out
	if step.Input != "" {
		cmd.Stdin = strings.NewReader(step.Input)
	} else {
		cmd.Stdin = os.Stdin
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", step.String(), err)
	}
	return nil
}

// writeRootFile writes data at path as root. A non-root process stages the
// bytes in a temporary file and copies them with install(1) through sudo.
func writeRootFile(ctx context.Context, path string, data []byte, host Host, out io.Writer) error {
	if isRoot() {
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
	}, host, out)
}

func isRoot() bool {
	return os.Geteuid() == 0
}

// rootAvailable reports whether a step that needs root can run. Without a
// terminal, sudo can only run when it does not ask for a password.
func rootAvailable(host Host) bool {
	if isRoot() {
		return true
	}
	if _, err := exec.LookPath("sudo"); err != nil {
		return false
	}
	return host.Interactive || passwordlessSudo()
}

var passwordlessSudo = sync.OnceValue(func() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "sudo", "-n", "true").Run() == nil
})
