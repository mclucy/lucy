package cli

import (
	"fmt"

	"github.com/mclucy/lucy/catalog"
	serverpkg "github.com/mclucy/lucy/server"

	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/report"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/identify"
)

type GraphOrigin int

const (
	OriginLock GraphOrigin = iota
	OriginLive
)

func (ds GraphOrigin) String() string {
	switch ds {
	case OriginLock:
		return "lock file"
	case OriginLive:
		return "live probe"
	default:
		return "unknown"
	}
}

// ServerEnvironment is the resolved server identity a graph belongs to.
// Values come from the manifest or lock when either is readable; a live
// probe fills the gaps when no lock exists.
type ServerEnvironment struct {
	Core             string
	GameVersion      string
	Ecosystem        catalog.Ecosystem
	EcosystemVersion string
}

// LoadDependencyData builds the dependency graph from the lock file, falling
// back to a live workspace observation when the lock is missing or invalid.
// forceLive skips the lock file entirely.
//
// It also reports the server environment, derived from the manifest or lock
// when available and from the workspace probe otherwise.
func LoadDependencyData(workDir string, forceLive bool) (
	*DependencyGraph,
	GraphOrigin,
	ServerEnvironment,
	error,
) {
	var (
		env    ServerEnvironment
		lock   *lockfile.Document
		origin = OriginLock
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
		return graph, origin, env, nil
	}

	origin = OriginLive
	info, _ := identify.Packages(workspace.NewAt(workDir))
	reportObservationFindings(info)
	graph, err := BuildGraphFromProbe(info)
	if err != nil {
		return nil, 0, env, fmt.Errorf("failed to build graph from probe: %w", err)
	}
	return graph, origin, environmentFromWorkspace(info, env), nil
}

// reportObservationFindings renders each consumed observation once.
func reportObservationFindings(observed workspace.Workspace) {
	for _, warning := range observed.Warnings {
		report.Warn(warning)
	}
	for _, ambiguityErr := range observed.Probe.AmbiguityErrors(observed.ServerDir) {
		report.Error(ambiguityErr)
	}
	reportRuntimeFindings(observed.Probe.Findings)
}

func reportRuntimeFindings(findings []workspace.RuntimeFinding) {
	for _, finding := range findings {
		label := "unmanaged entry point"
		if finding.Kind == workspace.RuntimeUnmanaged {
			label = "unmanaged server entry point"
		}
		report.Info(fmt.Sprintf(
			"%s %s (%s %s / Minecraft %s); left unchanged",
			label, finding.Path, finding.Runtime.Product(), finding.Runtime.Primary.Version,
			finding.Runtime.MinecraftVersion(),
		))
	}
}

// environmentFromLock reads the server identity from the lock's explicit
// fields. The lock always records exact versions, so nothing is inferred.
func environmentFromLock(lock *lockfile.Document) ServerEnvironment {
	return ServerEnvironment{
		Core:             lock.Server.Product.String(),
		GameVersion:      lock.Server.Minecraft.String(),
		Ecosystem:        ecosystemFromLock(lock),
		EcosystemVersion: ecosystemVersionFromLock(lock),
	}
}

// ecosystemFromLock reports the ecosystem that scopes the server runtime.
// Server packages come first; the bootstrap inputs name the loader when the
// runtime has no packages of its own. It returns EcosystemUnspecified when
// the evidence disagrees.
func ecosystemFromLock(lock *lockfile.Document) catalog.Ecosystem {
	var found catalog.Ecosystem
	for _, pkg := range lock.Packages {
		if pkg.Environment != serverpkg.EnvironmentServer ||
			pkg.Ecosystem == catalog.EcosystemUnspecified {
			continue
		}
		if found != catalog.EcosystemUnspecified && found != pkg.Ecosystem {
			return catalog.EcosystemUnspecified
		}
		found = pkg.Ecosystem
	}
	if found != catalog.EcosystemUnspecified {
		return found
	}
	for _, input := range lock.Server.Inputs {
		for _, module := range input.Modules {
			if module.Ecosystem == catalog.EcosystemUnspecified {
				continue
			}
			if found != catalog.EcosystemUnspecified && found != module.Ecosystem {
				return catalog.EcosystemUnspecified
			}
			found = module.Ecosystem
		}
	}
	return found
}

func ecosystemVersionFromLock(lock *lockfile.Document) string {
	ecosystem := ecosystemFromLock(lock)
	if ecosystem == catalog.EcosystemUnspecified {
		return ""
	}
	for _, input := range lock.Server.Inputs {
		for _, module := range input.Modules {
			if module.Ecosystem == ecosystem && module.Version != "" {
				return module.Version.String()
			}
		}
	}
	return ""
}

// environmentFromWorkspace fills the environment gaps a lock could not
// supply by observing the live workspace. Values already known win.
func environmentFromWorkspace(ws workspace.Workspace, env ServerEnvironment) ServerEnvironment {
	if env.Core == "" || env.GameVersion == "" ||
		env.Ecosystem == catalog.EcosystemUnspecified {
		if doc, err := manifest.Read(ws.ServerDir); err == nil {
			if env.Core == "" {
				env.Core = doc.Server.Core.Product.String()
			}
			if env.GameVersion == "" {
				env.GameVersion = doc.Server.Minecraft.String()
			}
		}
	}
	server := ws.Runtime()
	if server == nil {
		return env
	}
	if env.GameVersion == "" {
		if version := server.MinecraftVersion().String(); version != "" {
			env.GameVersion = version
		}
	}
	if env.Core == "" {
		env.Core = server.Product().String()
	}
	if env.Ecosystem == catalog.EcosystemUnspecified {
		env.Ecosystem = server.Ecosystem()
	}
	if env.EcosystemVersion == "" {
		ecosystem := server.Ecosystem()
		for _, component := range server.Components {
			if component.Ecosystem != ecosystem {
				continue
			}
			switch component.Version {
			case "", versionpkg.None, versionpkg.Unknown, versionpkg.Any,
				versionpkg.Stable, versionpkg.Beta:
				continue
			default:
				env.EcosystemVersion = component.Version.String()
			}
		}
	}
	return env
}
