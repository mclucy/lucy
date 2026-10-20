// Package workspace probes a Minecraft server directory (i.e., a workspace).
//
// The package collects the following information:
//
//   - the server runtime that runs in the directory
//   - the directories that hold content packages
//   - the content packages that are installed
//   - the running state of the server process
//
// New, NewAt, and Refresh return a Workspace value. A Workspace is an
// immutable observation. Five members store facts: ServerDir, Wrappers,
// Probe, Packages, and Warnings. Every other member is a method over these
// facts.
//
// The package holds one cached observation of the current working
// directory. Only Rebuild, Invalidate, and Refresh change it; no other
// function keeps state between calls.
package workspace

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"sync"

	"github.com/mclucy/lucy/types"
)

const sessionLockName = "session.lock"

// Workspace is one consistent observation of a server directory.
type Workspace struct {
	// ServerDir is the server directory that the observation describes.
	// If MCDR manages the anchor, then ServerDir is the MCDR working
	// directory.
	ServerDir string            `json:"root"`
	Wrappers  types.WrapperInfo `json:"wrappers"`

	// Warnings records the recoverable problems seen while observing the
	// directory, such as MCDR version-detection failures. It stays out of
	// the JSON wire format; CLI presentation reports the entries once per
	// consumed observation.
	Warnings []error `json:"-"`

	// Probe holds the scan results before interpretation. The Probe type
	// documents the bucket invariant; read Probe when the single
	// interpreted server is not enough.
	Probe Probe `json:"-"`

	// Packages lists the discovered content packages, derived once during
	// observe.
	Packages []types.DiscoveredPackage `json:"packages"`
}

// MarshalJSON writes the wire format of an observation: the stored facts
// and the derived method values. Consumers of `lucy status --json` read
// this format. Probe is omitted; the raw scan is internal to this process.
func (ws Workspace) MarshalJSON() ([]byte, error) {
	projection := struct {
		ServerDir   string                    `json:"root"`
		SavePath    string                    `json:"save_path"`
		PackageDirs []string                  `json:"package_dirs"`
		Packages    []types.DiscoveredPackage `json:"packages"`
		Server      *ServerRuntime            `json:"server,omitempty"`
		Activity    *wireActivity             `json:"activity,omitempty"`
		Wrappers    types.WrapperInfo         `json:"wrappers"`
	}{
		ServerDir:   ws.ServerDir,
		SavePath:    ws.SaveDir(),
		PackageDirs: ws.PackageDirs(),
		Packages:    ws.Packages,
		Wrappers:    ws.Wrappers,
	}
	if server := ws.Runtime(); server != nil {
		projection.Server = server
		projection.Activity = &wireActivity{Running: ws.Running()}
	}
	return json.Marshal(projection)
}

type wireActivity struct {
	Running bool `json:"active"`
}

var (
	mu    sync.RWMutex
	cache Workspace
	ready bool
)

// New returns the observation of the process working directory. New builds
// the observation on the first call. Later calls read the cache.
func New() Workspace {
	mu.RLock()
	if ready {
		cached := cache
		mu.RUnlock()
		return cached
	}
	mu.RUnlock()

	root, err := os.Getwd()
	if err != nil {
		return Workspace{}
	}
	return rebuild(root)
}

// Rebuild builds the observation of the process working directory again.
// Readers wait until the build finishes.
func Rebuild() {
	root, err := os.Getwd()
	if err != nil {
		return
	}
	rebuild(root)
}

func rebuild(root string) Workspace {
	observed := observe(root)
	mu.Lock()
	cache = observed
	ready = true
	mu.Unlock()
	return observed
}

// Invalidate marks the cached observation as stale. The next New call
// probes the directory again.
func Invalidate() {
	mu.Lock()
	ready = false
	mu.Unlock()
}

// NewAt observes the directory workDir. NewAt does not read or write the
// process-global cache. Concurrent calls are safe.
func NewAt(workDir string) Workspace {
	target, err := filepath.Abs(workDir)
	if err != nil {
		return Workspace{}
	}
	return observe(target)
}

// Refresh observes the directory workDir again. If workDir resolves to the
// current working directory, Refresh replaces the cached observation.
// Otherwise Refresh returns a new observation and does not change the
// cache.
func Refresh(workDir string) Workspace {
	target, err := filepath.Abs(workDir)
	if err != nil {
		return Workspace{}
	}
	observed := observe(target)

	if current, err := os.Getwd(); err == nil && sameProbePath(target, current) {
		mu.Lock()
		cache = observed
		ready = true
		mu.Unlock()
	}
	return observed
}

// observe collects one snapshot of the server directory at dir. MCDR can
// move the server directory away from dir; the probe and package scan then
// run against that directory.
func observe(dir string) Workspace {
	wrappers, warnings := detectWrappers(dir)

	ws := Workspace{
		ServerDir: resolveServerDir(dir, wrappers),
		Wrappers:  wrappers,
		Warnings:  warnings,
	}
	ws.Probe = probeDirectory(ws.ServerDir)
	ws.Packages = discoverPackages(
		ws.PackageDirs(),
		mcdrPluginDirs(wrappers),
	)
	return ws
}

// resolveServerDir returns the directory that holds the server. Only MCDR
// moves the server away from the anchor. MCDR uses its configured working
// directory.
func resolveServerDir(anchor string, wrappers types.WrapperInfo) string {
	if wrappers.MCDR != nil {
		wd := wrappers.MCDR.Config.WorkingDirectory
		if filepath.IsAbs(wd) {
			return wd
		}
		return filepath.Join(anchor, wd)
	}
	return anchor
}

func mcdrPluginDirs(wrappers types.WrapperInfo) []string {
	if wrappers.MCDR == nil {
		return nil
	}
	return wrappers.MCDR.Config.PluginDirectories
}

// Runtime returns the single bootable runtime of the directory. Runtime
// returns nil in these cases:
//
//   - the scan found no runtime
//   - the scan found more than one runtime
//   - two or more detectors claim one jar
//
// Every non-nil result satisfies IsValid.
func (ws Workspace) Runtime() *ServerRuntime {
	server, _ := ws.Probe.Single()
	return server
}

// Running reports whether the server runs at this moment. The check probes
// an exclusive lock on the session file. Running performs I/O on every call,
// because activity changes over time. Running returns false when no single
// runtime exists, or when the save location is unknown.
func (ws Workspace) Running() bool {
	if ws.Runtime() == nil {
		return false
	}
	saveDir := ws.SaveDir()
	if saveDir == "" {
		return false
	}
	return checkSessionLock(filepath.Join(saveDir, sessionLockName))
}

// sameProbePath compares two directories after symlink resolution,
// treating unresolvable paths literally.
func sameProbePath(left, right string) bool {
	leftEval, leftErr := filepath.EvalSymlinks(left)
	if leftErr != nil {
		leftEval = left
	}
	rightEval, rightErr := filepath.EvalSymlinks(right)
	if rightErr != nil {
		rightEval = right
	}
	return leftEval == rightEval
}
