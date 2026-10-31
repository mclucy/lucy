package javainstall

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/internal/toolchain"
	"github.com/mclucy/lucy/log"
)

// probeTimeout bounds a read-only package manager query. A query that exceeds
// it counts as a missing package.
const probeTimeout = 30 * time.Second

// Policy carries the command boundary's choices for a Java installation.
type Policy struct {
	// Interactive reports that a terminal can answer password and agreement
	// prompts.
	Interactive bool
	// AllowRepository records the user's consent to add a vendor package
	// repository to the system package manager.
	AllowRepository bool
}

// Command is one process invocation for a System runner. Args[0] names the
// executable.
type Command struct {
	Args   []string
	Env    []string
	Dir    string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// System is the process and discovery boundary that one Java host uses. The
// default adapter is NewSystem; tests or alternate environments supply their
// own values without mutable package state. NewHost does not fill missing
// fields, so a supplied System must set every function it will use.
type System struct {
	// GOOS and GOARCH name the platform the host plans for.
	GOOS   string
	GOARCH string
	// LookPath resolves an executable name on PATH.
	LookPath func(name string) (string, error)
	// Run executes one command and waits for it.
	Run func(ctx context.Context, command Command) error
	// Getenv reads one environment variable.
	Getenv func(name string) string
	// Environ returns the process environment entries.
	Environ func() []string
	// UserHomeDir returns the current user's home directory.
	UserHomeDir func() (string, error)
	// ManagedRoot returns the per-user directory that holds downloaded Java
	// runtimes.
	ManagedRoot func() (string, error)
	// Release returns the fields of /etc/os-release.
	Release func() (map[string]string, error)
	// IsRoot reports whether Lucy runs as root.
	IsRoot func() bool
	// EvalSymlinks resolves a symbolic link to its target.
	EvalSymlinks func(path string) (string, error)
	// UsesMusl reports whether the host runs musl instead of glibc.
	UsesMusl func() bool
}

// NewSystem returns the real process and discovery boundary.
func NewSystem() System {
	return System{
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
		LookPath:     exec.LookPath,
		Run:          runCommand,
		Getenv:       os.Getenv,
		Environ:      os.Environ,
		UserHomeDir:  os.UserHomeDir,
		ManagedRoot:  toolchain.ManagedJavaRoot,
		Release:      readSystemRelease,
		IsRoot:       func() bool { return os.Geteuid() == 0 },
		EvalSymlinks: filepath.EvalSymlinks,
		UsesMusl:     detectMusl,
	}
}

func runCommand(ctx context.Context, command Command) error {
	if len(command.Args) == 0 {
		return errors.New("empty command")
	}
	process := exec.CommandContext(
		ctx,
		command.Args[0],
		command.Args[1:]...,
	)
	process.Env = command.Env
	process.Dir = command.Dir
	process.Stdin = command.Stdin
	process.Stdout = command.Stdout
	process.Stderr = command.Stderr
	return process.Run()
}

func readSystemRelease() (map[string]string, error) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return nil, err
	}
	fields := map[string]string{}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		fields[key] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return fields, nil
}

func detectMusl() bool {
	if _, err := os.Stat("/etc/alpine-release"); err == nil {
		return true
	}
	for _, pattern := range []string{
		"/lib/ld-musl-*.so.1",
		"/usr/lib/ld-musl-*.so.1",
		"/lib64/ld-musl-*.so.1",
	} {
		matches, err := filepath.Glob(pattern)
		if err == nil && len(matches) > 0 {
			return true
		}
	}
	return false
}

// Host is the per-invocation host boundary for Java provisioning. It finds
// executables, runs read-only probes, names the managed runtime directory,
// reads system release and elevation facts, and reaches the network through
// the invocation's cache client. Planning and execution share one Host, and
// every fact is probed only when a plan needs it. A Host must not be copied
// after first use.
type Host struct {
	network *cache.Client
	system  System
	logger  *log.Logger
	goos    string
	goarch  string

	managedOnce sync.Once
	managedPath string
	managedErr  error

	releaseOnce sync.Once
	release     map[string]string

	muslOnce sync.Once
	musl     bool

	rootOnce sync.Once
	root     bool

	sudoMu    sync.Mutex
	sudoKnown bool
	sudo      bool
}

// NewHost returns the host boundary for one invocation. It probes nothing
// until planning or execution needs a fact. system supplies the process and
// discovery boundary, network carries the invocation's HTTP transport and
// cache, and logger records planning decisions; a nil logger records nothing.
func NewHost(
	network *cache.Client,
	system System,
	logger *log.Logger,
) *Host {
	return &Host{
		network: network,
		system:  system,
		logger:  logger,
		goos:    system.GOOS,
		goarch:  system.GOARCH,
	}
}

// managedRoot returns the per-user directory that holds downloaded Java
// runtimes.
func (h *Host) managedRoot() (string, error) {
	h.managedOnce.Do(func() {
		h.managedPath, h.managedErr = h.system.ManagedRoot()
	})
	return h.managedPath, h.managedErr
}

// systemRelease returns the fields of /etc/os-release. An absent file yields
// an empty map.
func (h *Host) systemRelease() map[string]string {
	h.releaseOnce.Do(func() {
		release, err := h.system.Release()
		if err != nil {
			h.logger.Debug("read os-release failed", "reason", err)
			release = nil
		}
		if release == nil {
			release = map[string]string{}
		}
		h.release = release
	})
	return h.release
}

// usesMusl reports whether the host runs musl instead of glibc.
func (h *Host) usesMusl() bool {
	h.muslOnce.Do(func() {
		h.musl = h.system.UsesMusl()
	})
	return h.musl
}

// isRoot reports whether Lucy runs as root.
func (h *Host) isRoot() bool {
	h.rootOnce.Do(func() {
		h.root = h.system.IsRoot()
	})
	return h.root
}

// rootAvailable reports whether a step that needs root can run. Without a
// terminal, sudo can only run when it does not ask for a password.
func (h *Host) rootAvailable(ctx context.Context, policy Policy) (bool, error) {
	if h.isRoot() {
		return true, nil
	}
	path, err := h.lookPath("sudo")
	if err != nil {
		return false, nil
	}
	if policy.Interactive {
		return true, nil
	}
	return h.passwordlessSudo(ctx, path)
}

func (h *Host) passwordlessSudo(ctx context.Context, sudo string) (bool, error) {
	h.sudoMu.Lock()
	defer h.sudoMu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if h.sudoKnown {
		return h.sudo, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := h.system.Run(probeCtx, Command{
		Args: []string{sudo, "-n", "true"},
		Env:  h.environ(),
	})
	if err := probeCtx.Err(); err != nil {
		return false, err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false, err
	}
	h.sudo = err == nil
	h.sudoKnown = true
	return h.sudo, nil
}

// systemToolAvailable reports whether every named binary is on PATH. System
// package managers run only on Linux.
func (h *Host) systemToolAvailable(names ...string) bool {
	if h.goos != "linux" {
		return false
	}
	for _, name := range names {
		if _, err := h.lookPath(name); err != nil {
			return false
		}
	}
	return true
}

// systemProbe runs a read-only query with a bounded wait. The error carries
// stderr so the caller can report why the query failed.
func (h *Host) systemProbe(ctx context.Context, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("empty system probe")
	}
	return h.probe(ctx, probeTimeout, nil, args[0], args[1:]...)
}

// probe runs one read-only command with a bounded wait. A nil environment
// uses the supplied system environment.
func (h *Host) probe(
	ctx context.Context,
	wait time.Duration,
	environment []string,
	name string,
	args ...string,
) ([]byte, error) {
	probeCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	path, err := h.lookPath(name)
	if err != nil {
		return nil, err
	}
	if environment == nil {
		environment = h.environ()
	}
	var stdout, stderr bytes.Buffer
	err = h.system.Run(probeCtx, Command{
		Args:   append([]string{path}, args...),
		Env:    environment,
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		line := strings.Join(append([]string{name}, args...), " ")
		if detail == "" {
			return stdout.Bytes(), fmt.Errorf("%s: %w", line, err)
		}
		return stdout.Bytes(), fmt.Errorf("%s: %w: %s", line, err, detail)
	}
	return stdout.Bytes(), nil
}

func (h *Host) lookPath(name string) (string, error) {
	return h.system.LookPath(name)
}

func (h *Host) getenv(name string) string {
	return h.system.Getenv(name)
}

func (h *Host) environ() []string {
	return h.system.Environ()
}

func (h *Host) userHomeDir() (string, error) {
	return h.system.UserHomeDir()
}

// fetchMetadata reads a metadata URL through the invocation's cache.
func (h *Host) fetchMetadata(
	ctx context.Context,
	address string,
	ttl time.Duration,
) ([]byte, error) {
	return h.network.CachedGetBytes(ctx, address, cache.BytesRequestOptions{
		Kind: cache.KindMetadata,
		TTL:  ttl,
	})
}

// request issues one HTTP request through the invocation's client.
func (h *Host) request(
	ctx context.Context,
	address string,
) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	return h.network.Do(request)
}
