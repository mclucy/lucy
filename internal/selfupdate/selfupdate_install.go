package selfupdate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Installation describes where the running executable came from and whether
// this command may replace it.
type Installation struct {
	Executable string
	Method     string
	Managed    bool
	Updatable  bool
	Blocked    bool
	Guidance   string
	Notes      []string
}

func (s *Service) detect(opts Options, probe bool) (Installation, error) {
	executable := opts.Executable
	if executable == "" {
		found, err := os.Executable()
		if err != nil {
			return Installation{}, fmt.Errorf("locate the lucy executable: %w", err)
		}
		executable = found
	}
	executable, err := filepath.Abs(executable)
	if err != nil {
		return Installation{}, fmt.Errorf("resolve the lucy executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}

	path := filepath.ToSlash(executable)
	installation := Installation{Executable: executable}
	switch {
	case strings.Contains(path, "/Caskroom/lucy/"), strings.Contains(path, "/Cellar/lucy/"):
		installation.Method = "Homebrew"
		installation.Managed = true
		installation.Guidance = "run `brew upgrade --cask lucy`"
	case strings.HasPrefix(path, "/nix/store/"):
		installation.Method = "Nix"
		installation.Managed = true
		installation.Guidance = "upgrade the Nix profile or flake input"
	case inContainer():
		installation.Method = "container"
		installation.Managed = true
		installation.Guidance = "pull the latest image instead"
	case !opts.Official:
		installation.Method = "source"
		installation.Guidance = "rebuild it with `task build`, reinstall it with `go install github.com/mclucy/lucy@latest`, or use --force"
	default:
		installation.Method = "archive"
		if !probe {
			installation.Updatable = true
			break
		}
		if err := probeDirectory(filepath.Dir(executable)); err != nil {
			installation.Blocked = true
			installation.Guidance = fmt.Sprintf(
				"cannot write to %s: %v; install lucy in a user-writable directory",
				filepath.Dir(executable),
				err,
			)
			return installation, nil
		}
		installation.Updatable = true
	}
	if !installation.Updatable {
		installation.Notes = append(installation.Notes, installation.Guidance)
	}
	return installation, nil
}

func inContainer() bool {
	for _, path := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func probeDirectory(dir string) error {
	file, err := os.CreateTemp(dir, ".lucy-update-probe-*")
	if err != nil {
		return err
	}
	name := file.Name()
	closeErr := file.Close()
	removeErr := os.Remove(name)
	return firstError(closeErr, removeErr)
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
