package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
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

// mcdrProvisionError explains how to provide a Python environment when
// provisioning fails.
func mcdrProvisionError(cause error) error {
	return fmt.Errorf(
		"%w\n"+
			"MCDReforged needs Python 3.8+ or uv to create an isolated "+
			"environment. Install Python 3 or uv, then rerun "+
			"`lucy install --mcdr-venv`",
		cause,
	)
}

func mcdrEnvironmentKey(
	goos string,
	goarch string,
	mcdrVersion string,
	requirements []string,
) string {
	hash := sha256.New()
	for _, part := range append(
		[]string{mcdrVersion, goos, goarch, "venv"},
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
	python *PythonHost,
	useVenv bool,
) (mcdrRuntime, error) {
	m := d.MCDR
	if m == nil {
		return mcdrRuntime{}, nil
	}
	if !useVenv {
		path, err := python.LookPath("mcdreforged")
		if err != nil {
			return mcdrRuntime{}, mcdrGlobalError(`"mcdreforged"`)
		}
		if _, _, err := python.Interpreter(ctx); err != nil {
			return mcdrRuntime{}, fmt.Errorf(
				"MCDReforged requires Python 3.8+ on PATH: %w. "+
					"Install Python 3, or %s",
				err,
				mcdrFlagHint(),
			)
		}
		output, err := python.Run(ctx, path, []string{"--version"}, nil)
		if err != nil {
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
	key := mcdrEnvironmentKey(
		python.GOOS,
		python.GOARCH,
		string(m.Version),
		requirements,
	)
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
	backend, err := createMCDREnvironment(ctx, environment, store, python)
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
		python,
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
	python *PythonHost,
) (string, error) {
	var uvError error
	if uv, err := python.LookPath("uv"); err == nil {
		captured, err := python.Run(
			ctx,
			uv,
			[]string{"venv", "--seed", environment},
			store.trace.processOutput(),
		)
		if err == nil {
			return "uv", nil
		}
		uvError = fmt.Errorf("uv venv: %w\n%s", err, captured)
		python.Logger.Warn(uvError)
	}
	interpreter, _, err := python.Interpreter(ctx)
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
	captured, err := python.Run(
		ctx,
		interpreter,
		[]string{"-m", "venv", environment},
		store.trace.processOutput(),
	)
	if err != nil {
		return "", fmt.Errorf(
			"create Python environment with %s: %w\n%s",
			interpreter,
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
	python *PythonHost,
) error {
	environmentPython := filepath.Join(environment, "bin", "python")
	if python.GOOS == "windows" {
		environmentPython = filepath.Join(environment, "Scripts", "python.exe")
	}
	var commandName string
	var arguments []string
	if backend == "uv" {
		uv, err := python.LookPath("uv")
		if err != nil {
			return err
		}
		commandName = uv
		arguments = []string{
			"pip",
			"install",
			"--python",
			environmentPython,
			"--requirement",
			requirementsPath,
		}
	} else {
		commandName = environmentPython
		arguments = []string{
			"-m",
			"pip",
			"install",
			"--requirement",
			requirementsPath,
		}
	}
	captured, err := python.Run(
		ctx,
		commandName,
		arguments,
		store.trace.processOutput(),
	)
	if err != nil {
		return fmt.Errorf(
			"install Python requirements with %s: %w\n%s",
			backend,
			err,
			captured,
		)
	}
	return nil
}
