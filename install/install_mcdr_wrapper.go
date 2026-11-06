package install

import (
	"context"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/document"
	"gopkg.in/yaml.v3"
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

// ensureMCDRRuntime repairs the MCDReforged launcher when the rest of the
// installation is already current. The generated config is preserved unless
// it is missing.
func ensureMCDRRuntime(
	ctx context.Context,
	root string,
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
	return writeMCDRWrapper(root, d, runtime, false, python.GOOS)
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
		pythonPath := filepath.Join(".python", mcdr.key, "bin", "python")
		if goos == "windows" {
			pythonPath = filepath.Join(".python", mcdr.key, "Scripts", "python.exe")
		}
		launcher = "'" + filepath.ToSlash(pythonPath) + "' -m mcdreforged"
	}
	script := "#!/bin/sh\ncd -- \"$(dirname -- \"$0\")\"\nexec " +
		launcher + " \"$@\"\n"
	return os.WriteFile(
		filepath.Join(wrapper, "run-mcdr.sh"),
		[]byte(script),
		0o755,
	)
}
