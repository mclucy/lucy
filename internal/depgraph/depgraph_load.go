package depgraph

import (
	"context"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	serverpkg "github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
	"github.com/mclucy/lucy/workspace/identify"
)

// Origin names where a loaded graph came from.
type Origin int

const (
	OriginLock Origin = iota
	OriginLive
)

func (o Origin) String() string {
	switch o {
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

// Findings is the live observation evidence a graph was built from. It
// carries no presentation decisions; the command boundary renders its
// warnings and discovery conditions.
type Findings struct {
	// Observation is the probe the graph was built from. It is nil when the
	// graph came from the lock or no live probe ran.
	Observation *workspace.Workspace
}

// LoadResult is one graph and the facts that produced it.
type LoadResult struct {
	Graph       *Graph
	Origin      Origin
	Environment ServerEnvironment
	Findings    Findings
}

// Load builds the dependency graph from the lock file, falling back to a live
// workspace observation when the lock is missing or invalid. forceLive skips
// the lock file entirely.
//
// It also reports the server environment, derived from the manifest or lock
// when available and from the workspace probe otherwise. A live probe is
// returned as findings; the caller renders it.
func Load(
	ctx context.Context,
	workDir string,
	forceLive bool,
	lookup identify.HashLookup,
) (LoadResult, error) {
	var result LoadResult
	if err := ctx.Err(); err != nil {
		return result, err
	}

	if !forceLive {
		doc, err := document.LockFile(workDir).Read()
		if err == nil {
			result.Environment = environmentFromLock(doc)
			graph, err := BuildFromLock(doc)
			if err != nil {
				return result, fmt.Errorf(
					"failed to build graph from lock: %w",
					err,
				)
			}
			if err := ctx.Err(); err != nil {
				return result, err
			}
			result.Graph = graph
			return result, nil
		}
		// Lock file missing or invalid — fall through to probe.
	}

	result.Origin = OriginLive
	info, _ := identify.Packages(
		ctx,
		workspace.Observe(workDir, discovery.Request{}),
		lookup,
	)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Findings = Findings{Observation: info}
	graph, err := BuildFromObservation(info)
	if err != nil {
		return result, fmt.Errorf("failed to build graph from observation: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Graph = graph
	result.Environment = environmentFromWorkspace(info, result.Environment)
	return result, nil
}

// environmentFromLock reads the server identity from the lock's explicit
// fields. The lock always records exact versions, so nothing is inferred.
func environmentFromLock(lock *document.Lock) ServerEnvironment {
	return ServerEnvironment{
		Core:             lock.Server.Product.String(),
		GameVersion:      lock.Server.Minecraft.String(),
		Ecosystem:        EcosystemFromLock(lock),
		EcosystemVersion: ecosystemVersionFromLock(lock),
	}
}

// EcosystemFromLock reports the ecosystem that scopes the server runtime.
// Server packages come first; the bootstrap inputs name the loader when the
// runtime has no packages of its own. It returns EcosystemUnspecified when
// the evidence disagrees.
func EcosystemFromLock(lock *document.Lock) catalog.Ecosystem {
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

func ecosystemVersionFromLock(lock *document.Lock) string {
	ecosystem := EcosystemFromLock(lock)
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
func environmentFromWorkspace(ws *workspace.Workspace, env ServerEnvironment) ServerEnvironment {
	if ws == nil {
		return env
	}
	if env.Core == "" || env.GameVersion == "" ||
		env.Ecosystem == catalog.EcosystemUnspecified {
		if doc, err := document.ManifestFile(ws.ServerDir).Read(); err == nil {
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
		for _, component := range RuntimeComponents(server) {
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

// RuntimeComponents normalizes the runtime identity into the versioned
// components it declares, including the primary runtime and Minecraft.
func RuntimeComponents(
	selection *discovery.Selection,
) []catalog.VersionedRef {
	if selection == nil || !selection.IsValid() {
		return []catalog.VersionedRef{}
	}
	identity := selection.Installation.Identity
	components := make([]catalog.VersionedRef, 0, len(identity.Components)+2)
	components = append(components, identity.Components...)

	primary := identity.Primary
	switch strings.ToLower(primary.Name.String()) {
	case "minecraft":
		components = ensureRuntimeComponent(components, catalog.VersionedRef{
			Ecosystem: catalog.EcosystemMinecraft,
			Name:      "minecraft",
			Version:   primary.Version,
		})
	default:
		if product, ok := serverpkg.ParseProduct(primary.Name.String()); ok {
			if component := product.LoaderComponent(); component != "" {
				components = ensureRuntimeComponent(components, catalog.VersionedRef{
					Ecosystem: product.Ecosystem(),
					Name:      component,
					Version:   primary.Version,
				})
			}
		}
	}
	if !hasRuntimeComponent(components, catalog.EcosystemMinecraft, "minecraft") {
		components = ensureRuntimeComponent(components, catalog.VersionedRef{
			Ecosystem: catalog.EcosystemMinecraft,
			Name:      "minecraft",
			Version:   identity.MinecraftVersion(),
		})
	}
	return components
}

func ensureRuntimeComponent(
	components []catalog.VersionedRef,
	component catalog.VersionedRef,
) []catalog.VersionedRef {
	if component.Version == "" || component.Version == versionpkg.Unknown {
		return components
	}
	if hasRuntimeComponent(components, component.Ecosystem, component.Name.String()) {
		return components
	}
	return append(components, component)
}

func hasRuntimeComponent(
	components []catalog.VersionedRef,
	ecosystem catalog.Ecosystem,
	name string,
) bool {
	for _, component := range components {
		if component.Ecosystem == ecosystem && component.Name == catalog.Name(name) {
			return true
		}
	}
	return false
}
