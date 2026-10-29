package install

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"gopkg.in/yaml.v3"
)

type mcdrRuntime struct {
	// venv reports whether the launcher uses the isolated environment.
	venv bool
	// key names the isolated environment. It is empty for a global runtime.
	key string
}

func mcdrFlagHint() string {
	return "pass --mcdr-venv to create an isolated environment"
}

func mcdrGlobalError(missing string) error {
	return fmt.Errorf(
		"MCDReforged is required for this workspace, but %s was not found "+
			"on PATH. Install MCDReforged and Python 3.8+ globally, or %s",
		missing,
		mcdrFlagHint(),
	)
}

func pythonInterpreter(ctx context.Context) (string, string, error) {
	name := strings.TrimSpace(os.Getenv("LUCY_PYTHON"))
	if name == "" {
		name = "python3"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", "", fmt.Errorf("Python interpreter %q was not found", name)
	}
	out, err := exec.CommandContext(
		ctx,
		path,
		"-c",
		"import sys;print('%d.%d.%d' % sys.version_info[:3])",
	).Output()
	if err != nil {
		return "", "", fmt.Errorf(
			"Python interpreter %s could not run: %w",
			path,
			err,
		)
	}
	value := strings.TrimSpace(string(out))
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return "", "", fmt.Errorf(
			"Python interpreter %s reported %q",
			path,
			value,
		)
	}
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	if major < 3 || (major == 3 && minor < 8) {
		return "", "", fmt.Errorf(
			"Python interpreter %s is %s; MCDReforged needs 3.8 or newer",
			path,
			value,
		)
	}
	if major > 3 || minor > 13 {
		log.Warn(fmt.Errorf(
			"MCDReforged is tested through Python 3.13; %s is Python %s and "+
				"some dependencies may not publish wheels for it",
			path,
			value,
		))
	}
	return path, value, nil
}

func mcdrProvisionError(cause error) error {
	return fmt.Errorf(
		"%w\n"+
			"MCDReforged needs Python 3.8+ or uv to create an isolated "+
			"environment. Install Python 3 or uv, then rerun "+
			"`lucy install --mcdr-venv`",
		cause,
	)
}

func mcdrEnvironmentKey(mcdrVersion string, requirements []string) string {
	hash := sha256.New()
	for _, part := range append(
		[]string{mcdrVersion, runtime.GOOS, runtime.GOARCH, "venv"},
		requirements...,
	) {
		io.WriteString(hash, part)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))[:32]
}

// provisionMCDR selects the runtime that will launch MCDReforged. The default
// requires a global executable; --mcdr-venv creates an isolated environment.
func provisionMCDR(
	ctx context.Context,
	root string,
	d *document.Lock,
	store *downloadStore,
	useVenv bool,
) (mcdrRuntime, error) {
	m := d.MCDR
	if m == nil {
		return mcdrRuntime{}, nil
	}
	if !useVenv {
		path, err := exec.LookPath("mcdreforged")
		if err != nil {
			return mcdrRuntime{}, mcdrGlobalError(`"mcdreforged"`)
		}
		if _, _, err := pythonInterpreter(ctx); err != nil {
			return mcdrRuntime{}, fmt.Errorf(
				"MCDReforged requires Python 3.8+ on PATH: %w. "+
					"Install Python 3, or %s",
				err,
				mcdrFlagHint(),
			)
		}
		if output, err := exec.CommandContext(
			ctx,
			path,
			"--version",
		).CombinedOutput(); err != nil {
			return mcdrRuntime{}, fmt.Errorf(
				"global MCDReforged at %s could not run: %w\n%s\n"+
					"Repair it, or %s",
				path,
				err,
				output,
				mcdrFlagHint(),
			)
		}
		return mcdrRuntime{}, nil
	}
	requirements := mcdrPythonRequirements(d)
	key := mcdrEnvironmentKey(string(m.Version), requirements)
	relative := filepath.Join(m.Directory, ".python", key)
	if err := workspacewrite.SafePath(root, relative); err != nil {
		return mcdrRuntime{}, err
	}
	environment := filepath.Join(root, relative)
	complete := filepath.Join(environment, ".complete")
	if _, err := os.Stat(complete); err == nil {
		return mcdrRuntime{venv: true, key: key}, nil
	} else if !os.IsNotExist(err) {
		return mcdrRuntime{}, err
	}
	if store.offline {
		return mcdrRuntime{}, fmt.Errorf(
			"MCDReforged isolated environment is not provisioned; run " +
				"`lucy install --mcdr-venv` once with network access",
		)
	}
	if err := os.RemoveAll(environment); err != nil {
		return mcdrRuntime{}, err
	}
	if err := os.MkdirAll(filepath.Dir(environment), 0o755); err != nil {
		return mcdrRuntime{}, err
	}
	backend, err := createMCDREnvironment(ctx, environment, store)
	if err != nil {
		return mcdrRuntime{}, mcdrProvisionError(err)
	}
	requirementsPath := filepath.Join(environment, "requirements.txt")
	if err := os.WriteFile(
		requirementsPath,
		[]byte(strings.Join(requirements, "\n")+"\n"),
		0o600,
	); err != nil {
		return mcdrRuntime{}, err
	}
	if err := installMCDRRequirements(
		ctx,
		environment,
		backend,
		requirementsPath,
		store,
	); err != nil {
		os.RemoveAll(environment)
		return mcdrRuntime{}, mcdrProvisionError(err)
	}
	if err := os.WriteFile(complete, nil, 0o600); err != nil {
		return mcdrRuntime{}, err
	}
	return mcdrRuntime{venv: true, key: key}, nil
}

func createMCDREnvironment(
	ctx context.Context,
	environment string,
	store *downloadStore,
) (string, error) {
	var uvError error
	if uv, err := exec.LookPath("uv"); err == nil {
		command := exec.CommandContext(ctx, uv, "venv", "--seed", environment)
		if captured, err := runProcess(
			command,
			store.trace.processOutput(),
		); err == nil {
			return "uv", nil
		} else {
			uvError = fmt.Errorf("uv venv: %w\n%s", err, captured)
			log.Warn(uvError)
		}
	}
	python, _, err := pythonInterpreter(ctx)
	if err != nil {
		if uvError != nil {
			return "", fmt.Errorf(
				"no usable Python or uv: %w; %w",
				err,
				uvError,
			)
		}
		return "", err
	}
	command := exec.CommandContext(ctx, python, "-m", "venv", environment)
	if captured, err := runProcess(
		command,
		store.trace.processOutput(),
	); err != nil {
		return "", fmt.Errorf(
			"create Python environment with %s: %w\n%s",
			python,
			err,
			captured,
		)
	}
	return "python", nil
}

func installMCDRRequirements(
	ctx context.Context,
	environment string,
	backend string,
	requirementsPath string,
	store *downloadStore,
) error {
	environmentPython := filepath.Join(environment, "bin", "python")
	if runtime.GOOS == "windows" {
		environmentPython = filepath.Join(environment, "Scripts", "python.exe")
	}
	var command *exec.Cmd
	if backend == "uv" {
		uv, err := exec.LookPath("uv")
		if err != nil {
			return err
		}
		command = exec.CommandContext(
			ctx,
			uv,
			"pip",
			"install",
			"--python",
			environmentPython,
			"--requirement",
			requirementsPath,
		)
	} else {
		command = exec.CommandContext(
			ctx,
			environmentPython,
			"-m",
			"pip",
			"install",
			"--requirement",
			requirementsPath,
		)
	}
	if captured, err := runProcess(
		command,
		store.trace.processOutput(),
	); err != nil {
		return fmt.Errorf(
			"install Python requirements with %s: %w\n%s",
			backend,
			err,
			captured,
		)
	}
	return nil
}

// preparePython selects the MCDReforged runtime and writes its wrapper into
// the stage. It runs while the installation is staged, before commit.
func preparePython(
	ctx context.Context,
	root string,
	stage string,
	d *document.Lock,
	store *downloadStore,
	useVenv bool,
) error {
	runtime, err := provisionMCDR(ctx, root, d, store, useVenv)
	if err != nil {
		return err
	}
	if d.MCDR == nil {
		return nil
	}
	return writeMCDRWrapper(stage, d, runtime, true)
}

// ensureMCDRRuntime repairs the MCDReforged launcher when the rest of the
// installation is already current. The generated config is preserved unless
// it is missing.
func ensureMCDRRuntime(
	ctx context.Context,
	root string,
	d *document.Lock,
	store *downloadStore,
	useVenv bool,
) error {
	runtime, err := provisionMCDR(ctx, root, d, store, useVenv)
	if err != nil {
		return err
	}
	if d.MCDR == nil {
		return nil
	}
	return writeMCDRWrapper(root, d, runtime, false)
}

func writeMCDRWrapper(
	base string,
	d *document.Lock,
	mcdr mcdrRuntime,
	replaceConfig bool,
) error {
	m := d.MCDR
	wrapper := filepath.Join(base, m.Directory)
	if err := os.MkdirAll(wrapper, 0o755); err != nil {
		return err
	}
	configPath := filepath.Join(wrapper, "config.yml")
	_, configErr := os.Stat(configPath)
	if replaceConfig || os.IsNotExist(configErr) {
		entry := "java -jar server.jar nogui"
		if d.Server.Bootstrapper == "fabric" || d.Server.Bootstrapper == "forge-installer" {
			entry = "sh run.sh"
		}
		relativeServer, err := filepath.Rel(m.Directory, d.Server.Directory)
		if err != nil {
			return err
		}
		// MCDReforged parses the server log stream through a handler that
		// matches the launcher, so a Bukkit-family server needs its own parser
		// to report start-up and player events at all.
		launchHandler := "vanilla_handler"
		switch d.Server.Bootstrapper {
		case "forge-installer":
			launchHandler = "forge_handler"
		case "paperclip":
			launchHandler = "bukkit_handler"
		}
		config := map[string]any{
			"working_directory": filepath.ToSlash(relativeServer),
			"start_command":     entry,
			"handler":           launchHandler,
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
		if runtime.GOOS == "windows" {
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

// runProcess streams a subprocess's combined output to output while retaining
// it for the error message.
func runProcess(cmd *exec.Cmd, output io.Writer) ([]byte, error) {
	if output == nil {
		output = io.Discard
	}
	var captured bytes.Buffer
	writer := io.MultiWriter(output, &captured)
	cmd.Stdout = writer
	cmd.Stderr = writer
	err := cmd.Run()
	return captured.Bytes(), err
}
