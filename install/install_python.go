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
	"sync"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"gopkg.in/yaml.v3"
)

// PythonHost is the process boundary for MCDReforged's Python provisioning. A
// command boundary constructs one per invocation, so interpreter discovery,
// the environment layout, and the warnings seen by every stage agree.
// LookPath, Output, and Run are its process boundaries.
type PythonHost struct {
	// LookPath resolves an executable name on PATH.
	LookPath func(name string) (string, error)
	// Output captures stdout separately from startup diagnostics on stderr.
	Output func(ctx context.Context, name string, args []string) ([]byte, error)
	// Run runs one command and returns its combined output. A non-nil output
	// also receives the output as it arrives.
	Run func(
		ctx context.Context,
		name string,
		args []string,
		output io.Writer,
	) ([]byte, error)
	// Getenv reads an environment variable. LUCY_PYTHON names the preferred
	// interpreter.
	Getenv func(string) string
	// GOOS and GOARCH name the platform whose environment layout the host
	// creates.
	GOOS   string
	GOARCH string
	// Logger records non-fatal provisioning warnings. A nil logger records
	// nothing.
	Logger *log.Logger

	directory string

	interpreterMu   sync.Mutex
	interpreterPath string
	interpreterVer  string
	interpreterOK   bool
}

// NewPythonHost returns the host boundary for one invocation. It probes no
// interpreter until MCDReforged provisioning needs one.
func NewPythonHost(directory string, logger *log.Logger) *PythonHost {
	if absolute, err := filepath.Abs(directory); err == nil {
		directory = absolute
	}
	return &PythonHost{
		directory: directory,
		LookPath:  exec.LookPath,
		Output: func(ctx context.Context, name string, args []string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Dir = directory
			return cmd.Output()
		},
		Run: func(
			ctx context.Context,
			name string,
			args []string,
			output io.Writer,
		) ([]byte, error) {
			cmd := exec.CommandContext(ctx, name, args...)
			cmd.Dir = directory
			return runProcess(cmd, output)
		},
		Getenv: os.Getenv,
		GOOS:   runtime.GOOS,
		GOARCH: runtime.GOARCH,
		Logger: logger,
	}
}

// Interpreter returns the Python 3.8+ executable and its version, probing at
// most once per host.
func (h *PythonHost) Interpreter(ctx context.Context) (string, string, error) {
	h.interpreterMu.Lock()
	defer h.interpreterMu.Unlock()
	if h.interpreterOK {
		return h.interpreterPath, h.interpreterVer, nil
	}
	path, version, err := h.discoverInterpreter(ctx)
	if err != nil {
		return "", "", err
	}
	h.interpreterPath, h.interpreterVer = path, version
	h.interpreterOK = true
	return path, version, nil
}

func (h *PythonHost) discoverInterpreter(
	ctx context.Context,
) (string, string, error) {
	name := strings.TrimSpace(h.Getenv("LUCY_PYTHON"))
	if name == "" {
		name = "python3"
	} else if !filepath.IsAbs(name) && (strings.ContainsRune(name, filepath.Separator) ||
		(h.GOOS == "windows" && strings.ContainsRune(name, '/'))) {
		name = filepath.Join(h.directory, name)
	}
	path, err := h.LookPath(name)
	if err != nil {
		return "", "", fmt.Errorf("Python interpreter %q was not found", name)
	}
	out, err := h.Output(
		ctx,
		path,
		[]string{
			"-c",
			"import sys;print('%d.%d.%d' % sys.version_info[:3])",
		},
	)
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
		h.Logger.Warn(fmt.Errorf(
			"MCDReforged is tested through Python 3.13; %s is Python %s and "+
				"some dependencies may not publish wheels for it",
			path,
			value,
		))
	}
	return path, value, nil
}

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
