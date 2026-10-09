package cli

import (
	"fmt"

	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
)

type DataSource int

const (
	SourceLock DataSource = iota
	SourceProbe
)

func (ds DataSource) String() string {
	switch ds {
	case SourceLock:
		return "lock file"
	case SourceProbe:
		return "live probe"
	default:
		return "unknown"
	}
}

// ServerEnvironment is the resolved server identity a graph belongs to.
// Values come from the manifest or lock when either is readable; a live
// probe fills the gaps when no lock exists.
type ServerEnvironment struct {
	Core        string
	GameVersion string
	Loader      types.Ecosystem
	LoaderVer   string
}

// LoadDependencyData builds the dependency graph from the lock file, falling
// back to a live workspace observation when the lock is missing or invalid.
// forceLive skips the lock file entirely.
//
// It also reports the server environment, derived from the manifest or lock
// when available and from the workspace probe otherwise.
func LoadDependencyData(workDir string, forceLive bool) (
	*DependencyGraph,
	DataSource,
	ServerEnvironment,
	error,
) {
	var (
		env    ServerEnvironment
		lock   *lockfile.Document
		source = SourceLock
	)

	if !forceLive {
		doc, err := lockfile.Read(workDir)
		if err == nil {
			lock = doc
			env = environmentFromLock(doc)
		}
		// Lock file missing or invalid — fall through to probe.
	}

	if lock != nil {
		graph, err := BuildGraphFromLock(lock)
		if err != nil {
			return nil, 0, env, fmt.Errorf(
				"failed to build graph from lock: %w",
				err,
			)
		}
		return graph, source, env, nil
	}

	source = SourceProbe
	info := workspace.NewAt(workDir)
	graph, err := BuildGraphFromProbe(info)
	if err != nil {
		return nil, 0, env, fmt.Errorf("failed to build graph from probe: %w", err)
	}
	return graph, source, environmentFromWorkspace(info, env), nil
}

// environmentFromLock reads the server identity from the lock's explicit
// fields. The lock always records exact versions, so nothing is inferred.
func environmentFromLock(lock *lockfile.Document) ServerEnvironment {
	return ServerEnvironment{
		Core:        lock.Server.Distribution,
		GameVersion: lock.Server.Minecraft,
		Loader:      loaderFromLock(lock),
		LoaderVer:   loaderVersionFromLock(lock),
	}
}

// loaderFromLock reports the loader that scopes the server runtime. Server
// packages come first; the bootstrap inputs name the loader when the runtime
// has no packages of its own. It returns EcoUnspecified when the evidence
// disagrees.
func loaderFromLock(lock *lockfile.Document) types.Ecosystem {
	var found types.Ecosystem
	for _, pkg := range lock.Packages {
		if pkg.Runtime != RuntimeServer || pkg.Loader == types.EcoUnspecified {
			continue
		}
		if found != types.EcoUnspecified && found != pkg.Loader {
			return types.EcoUnspecified
		}
		found = pkg.Loader
	}
	if found != types.EcoUnspecified {
		return found
	}
	for _, input := range lock.Server.Inputs {
		for _, module := range input.Modules {
			if module.Loader == types.EcoUnspecified {
				continue
			}
			if found != types.EcoUnspecified && found != module.Loader {
				return types.EcoUnspecified
			}
			found = module.Loader
		}
	}
	return found
}

func loaderVersionFromLock(lock *lockfile.Document) string {
	loader := loaderFromLock(lock)
	if loader == types.EcoUnspecified {
		return ""
	}
	for _, input := range lock.Server.Inputs {
		for _, module := range input.Modules {
			if module.Loader == loader && module.Version != "" {
				return module.Version
			}
		}
	}
	return ""
}

// environmentFromWorkspace fills the environment gaps a lock could not
// supply by observing the live workspace. Values already known win.
func environmentFromWorkspace(ws workspace.Workspace, env ServerEnvironment) ServerEnvironment {
	if env.Core == "" || env.GameVersion == "" || env.Loader == types.EcoUnspecified {
		if doc, err := manifest.Read(ws.Root); err == nil {
			if env.Core == "" {
				env.Core = doc.Server.Core.Distribution
			}
			if env.GameVersion == "" {
				env.GameVersion = doc.Server.Minecraft
			}
		}
	}
	server := ws.Server()
	if server == nil {
		return env
	}
	if env.GameVersion == "" {
		if version := server.GameVersion().String(); version != "" {
			env.GameVersion = version
		}
	}
	if env.Core == "" {
		env.Core = server.ServerCore()
	}
	if env.Loader == types.EcoUnspecified {
		env.Loader = server.ModLoader()
	}
	if env.LoaderVer == "" {
		loader := server.ModLoader()
		for _, component := range server.RuntimeComponents {
			if component.Eco != loader {
				continue
			}
			switch component.Version {
			case "", types.VersionNone, types.VersionUnknown, types.VersionAny,
				types.VersionStable, types.VersionBeta:
				continue
			default:
				env.LoaderVer = component.Version.String()
			}
		}
	}
	return env
}
