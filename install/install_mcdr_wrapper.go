package install

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/document"
	"go.yaml.in/yaml/v4"
)

// preparePython selects the MCDReforged runtime and writes its wrapper into
// the stage. It runs while the installation is staged, before commit, so a
// failed Python provisioning leaves the workspace untouched.
func preparePython(
	ctx context.Context,
	root string,
	stage string,
	d *document.Lock,
	store *downloadStore,
	python *PythonHost,
	useVenv bool,
) error {
	runtime, err := provisionMCDR(ctx, root, d, store, python, useVenv)
	if err != nil {
		return err
	}
	if d.MCDR == nil {
		return nil
	}
	return writeMCDRWrapper(stage, d, runtime, true, python.GOOS)
}

func writeMCDRWrapper(
	base string,
	d *document.Lock,
	mcdr mcdrRuntime,
	replaceConfig bool,
	goos string,
) error {
	m := d.MCDR
	wrapper := filepath.Join(base, m.Directory)
	if err := os.MkdirAll(wrapper, 0o755); err != nil {
		return err
	}
	configPath := filepath.Join(wrapper, "config.yml")
	_, configErr := os.Stat(configPath)
	if replaceConfig || os.IsNotExist(configErr) {
		launch := bootstrap.LaunchFor(d.Server.Bootstrapper)
		relativeServer, err := filepath.Rel(m.Directory, d.Server.Directory)
		if err != nil {
			return err
		}
		// MCDReforged parses the server log stream through a handler that
		// matches the launcher, so the method selects the parser.
		config := map[string]any{
			"working_directory": filepath.ToSlash(relativeServer),
			"start_command":     launch.Command,
			"handler":           launch.Handler,
		}
		data, err := yaml.Marshal(config)
		if err != nil {
			return err
		}
		if err := os.WriteFile(configPath, data, 0o644); err != nil {
			return err
		}
	} else if configErr != nil {
		return configErr
	}
	launcher := "mcdreforged"
	if mcdr.venv {
		launcher = mcdrVenvLauncher(mcdr.key, goos)
	}
	return os.WriteFile(
		filepath.Join(wrapper, "run-mcdr.sh"),
		mcdrWrapperLauncher(launcher),
		0o755,
	)
}

// mcdrVenvInterpreter returns the interpreter path, relative to the MCDR
// directory, of one isolated environment key.
func mcdrVenvInterpreter(key, goos string) string {
	pythonPath := filepath.Join(".python", key, "bin", "python")
	if goos == "windows" {
		pythonPath = filepath.Join(".python", key, "Scripts", "python.exe")
	}
	return filepath.ToSlash(pythonPath)
}

// mcdrVenvLauncher returns the command that runs MCDReforged from one isolated
// environment key.
func mcdrVenvLauncher(key, goos string) string {
	return "'" + mcdrVenvInterpreter(key, goos) + "' -m mcdreforged"
}

// mcdrWrapperCurrent reports whether the workspace wrapper already matches
// the launcher and seed the current lock would stage. It inspects files only,
// so metadata-only operations can decide whether runtime provisioning is
// needed without executing Python.
func mcdrWrapperCurrent(root string, d *document.Lock, python *PythonHost, useVenv bool) bool {
	if d == nil || d.MCDR == nil {
		return true
	}
	directory := filepath.Join(root, filepath.FromSlash(d.MCDR.Directory))
	configInfo, err := os.Stat(filepath.Join(directory, "config.yml"))
	if err != nil || !configInfo.Mode().IsRegular() {
		return false
	}
	launcher := "mcdreforged"
	if useVenv {
		key := mcdrEnvironmentKey(
			python.GOOS,
			python.GOARCH,
			string(d.MCDR.Version),
			mcdrPythonRequirements(d),
		)
		interpreter := filepath.Join(
			directory, filepath.FromSlash(mcdrVenvInterpreter(key, python.GOOS)),
		)
		info, err := os.Stat(interpreter)
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
		if _, err := os.Stat(filepath.Join(directory, ".python", key, ".complete")); err != nil {
			return false
		}
		launcher = mcdrVenvLauncher(key, python.GOOS)
	} else if _, err := python.LookPath("mcdreforged"); err != nil {
		return false
	}
	installed, err := os.ReadFile(filepath.Join(directory, "run-mcdr.sh"))
	if err != nil {
		return false
	}
	return bytes.Equal(installed, mcdrWrapperLauncher(launcher))
}

// mcdrWrapperLauncher renders the managed launcher script for one MCDR
// launcher command. Adoption compares an existing wrapper against the same
// bytes before granting ownership.
func mcdrWrapperLauncher(launcher string) []byte {
	return []byte(
		"#!/bin/sh\ncd -- \"$(dirname -- \"$0\")\"\nexec " +
			launcher + " \"$@\"\n",
	)
}

// mcdrLauncherVariant is one launcher script Lucy can generate for a locked
// MCDR. Interpreter is set for the isolated runtime and names the venv
// interpreter relative to the workspace, so adoption can require that the
// environment it launches still exists.
type mcdrLauncherVariant struct {
	Script      []byte
	Interpreter string
}

// mcdrLauncherVariants reports the exact wrapper scripts installation can
// produce for d on this machine: the global launcher and the isolated runtime
// whose environment key follows the provisioning algorithm.
func mcdrLauncherVariants(d *document.Lock) []mcdrLauncherVariant {
	if d == nil || d.MCDR == nil {
		return nil
	}
	m := d.MCDR
	variants := []mcdrLauncherVariant{{
		Script: mcdrWrapperLauncher("mcdreforged"),
	}}
	key := mcdrEnvironmentKey(
		runtime.GOOS,
		runtime.GOARCH,
		string(m.Version),
		mcdrPythonRequirements(d),
	)
	interpreter := filepath.Join(".python", key, "bin", "python")
	if runtime.GOOS == "windows" {
		interpreter = filepath.Join(".python", key, "Scripts", "python.exe")
	}
	variants = append(variants, mcdrLauncherVariant{
		Script: mcdrWrapperLauncher(
			"'" + filepath.ToSlash(interpreter) + "' -m mcdreforged",
		),
		Interpreter: filepath.ToSlash(
			filepath.Join(m.Directory, interpreter),
		),
	})
	return variants
}
