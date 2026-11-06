package install

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/mclucy/lucy/log"
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
