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
// immutable observation. Four members store facts: Root, Environments,
// Probe, and Packages. Every other member is a method over these facts.
//
// The package has one cache. The cache stores the observation of the
// current working directory. Only Rebuild, Invalidate, and Refresh change
// it. No other function keeps state between calls.
//
// TODO: remove direct dependency to upstream used by artifact hash queries
package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/mclucy/lucy/internal/knownpkgs"
	"github.com/mclucy/lucy/types"
)

const sessionLockName = "session.lock"

// Workspace is one consistent observation of a server directory.
type Workspace struct {
	// Root is the server directory that the observation describes.
	// If MCDR manages the anchor, then Root is the MCDR working directory.
	Root         string                `json:"root"`
	Environments types.EnvironmentInfo `json:"environments"`

	// Probe holds the scan results before interpretation. Each examined jar
	// appears in exactly one bucket. Read Probe when you need more than the
	// single interpreted server.
	Probe Probe `json:"-"`

	// Packages lists the discovered content packages. observe derives it
	// once from Probe and Environments. Artifact analysis makes this step
	// expensive.
	Packages []types.DiscoveredPackage `json:"packages"`
}

// MarshalJSON writes the wire format of an observation. The wire format
// contains the stored facts and the values of the derived methods.
// Consumers of `lucy status --json` read this format. MarshalJSON does not
// write Probe. The raw scan is for use inside this process only.
func (ws Workspace) MarshalJSON() ([]byte, error) {
	projection := struct {
		Root         string                    `json:"root"`
		SavePath     string                    `json:"save_path"`
		ModPath      []string                  `json:"mod_path"`
		Packages     []types.DiscoveredPackage `json:"packages"`
		Server       *ServerInstance           `json:"server,omitempty"`
		Activity     *wireActivity             `json:"activity,omitempty"`
		Environments types.EnvironmentInfo     `json:"environments"`
	}{
		Root:         ws.Root,
		SavePath:     ws.SaveDir(),
		ModPath:      ws.ModPath(),
		Packages:     ws.Packages,
		Environments: ws.Environments,
	}
	if server := ws.Server(); server != nil {
		projection.Server = server
		projection.Activity = &wireActivity{Active: ws.Active()}
	}
	return json.Marshal(projection)
}

type wireActivity struct {
	Active bool `json:"active"`
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

// observe collects one snapshot of the server directory at dir. The steps
// follow their data dependencies. First, detectEnvironment reads the host
// environments. MCDR can move the server root away from dir. Next,
// probeDirectory scans that root. Last, discoverPackages inventories the
// packages under the root.
func observe(dir string) Workspace {
	env := detectEnvironment(dir)

	ws := Workspace{
		Root:         resolveRoot(dir, env),
		Environments: env,
	}
	ws.Probe = probeDirectory(ws.Root)
	ws.Packages = discoverPackages(
		knownpkgs.Default().Session(),
		ws.ModPath(),
		mcdrPluginDirs(env),
	)
	return ws
}

// resolveRoot returns the directory that holds the server. Only MCDR moves
// the server away from the anchor. MCDR uses its configured working
// directory.
func resolveRoot(anchor string, env types.EnvironmentInfo) string {
	if env.Mcdr != nil {
		wd := env.Mcdr.Config.WorkingDirectory
		if filepath.IsAbs(wd) {
			return wd
		}
		return filepath.Join(anchor, wd)
	}
	return anchor
}

func mcdrPluginDirs(env types.EnvironmentInfo) []string {
	if env.Mcdr == nil {
		return nil
	}
	return env.Mcdr.Config.PluginDirectories
}

// Server returns the single bootable runtime of the directory. Server
// returns nil in these cases:
//
//   - the scan found no runtime
//   - the scan found more than one runtime
//   - two or more detectors claim one jar
//
// Every non-nil result satisfies IsValid.
func (ws Workspace) Server() *ServerInstance {
	server, _ := ws.Probe.Single()
	return server
}

// Active reports whether the server runs at this moment. The check probes
// an exclusive lock on the session file. Active performs I/O on every call,
// because activity changes over time. Active returns false when no single
// runtime exists, or when the save location is unknown.
func (ws Workspace) Active() bool {
	if ws.Server() == nil {
		return false
	}
	saveDir := ws.SaveDir()
	if saveDir == "" {
		return false
	}
	return checkSessionLock(filepath.Join(saveDir, sessionLockName))
}

// sameProbePath compares two directories after symlink resolution. Refresh
// uses it to detect alias paths of the current directory.
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
