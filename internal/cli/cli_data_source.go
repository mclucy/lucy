package cli

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	serverpkg "github.com/mclucy/lucy/server"

	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
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
func LoadDependencyData(
	ctx context.Context,
	workDir string,
	forceLive bool,
	lookup func(context.Context, upstream.ArtifactFingerprint) routing.ArtifactLookup,
	output *Output,
) (
	*DependencyGraph,
	GraphOrigin,
	ServerEnvironment,
	error,
) {
	var (
		env    ServerEnvironment
		lock   *document.Lock
		origin = OriginLock
	)
	if err := ctx.Err(); err != nil {
		return nil, 0, env, err
	}

	if !forceLive {
		doc, err := document.LockFile(workDir).Read()
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
		if err := ctx.Err(); err != nil {
			return nil, 0, env, err
		}
		return graph, origin, env, nil
	}

	origin = OriginLive
	info, _ := identify.Packages(
		ctx,
		workspace.Observe(workDir, discovery.Request{}),
		lookup,
	)
	if err := ctx.Err(); err != nil {
		return nil, 0, env, err
	}
	reportObservationFindings(info, output)
	graph, err := BuildGraphFromObservation(info)
	if err != nil {
		return nil, 0, env, fmt.Errorf("failed to build graph from observation: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, env, err
	}
	return graph, origin, environmentFromWorkspace(info, env), nil
}

// reportObservationFindings renders each consumed observation once.
func reportObservationFindings(observed *workspace.Workspace, output *Output) {
	if observed == nil {
		return
	}
	for _, warning := range observed.Warnings {
		output.Warn(warning)
	}
	reportDiscoveryFindings(&observed.Discovery, output)
}

// reportDiscoveryFindings presents one discovery result. A resolved layout
// never reports a failure: automatic choices are warnings, and unmanaged or
// alternative entry points stay informational.
func reportDiscoveryFindings(observed *discovery.Result, output *Output) {
	if observed == nil {
		return
	}
	selection := observed.Selection
	resolved := selection.IsValid()
	automatic := selection.Source == discovery.SourcePreferred
	unresolvedReported := false
	unreadableReported := map[string]bool{}
	for finding := range observed.Findings() {
		switch finding.Kind {
		case discovery.FindingAutomaticSelection:
			output.Warn(automaticSelectionMessage(observed, finding))
		case discovery.FindingUnreadableFile:
			key := unreadableFindingKey(finding)
			if unreadableReported[key] {
				continue
			}
			unreadableReported[key] = true
			output.Warn(unreadableFindingError(finding))
		case discovery.FindingAlternativeEntryPoint,
			discovery.FindingUnmanagedInstallation:
			message := entryPointFindingMessage(finding)
			if output.JSON() {
				output.Warn(errors.New(message))
			} else {
				output.Info(message)
			}
		case discovery.FindingMissingDeclaredInstallation:
			output.Warn(errors.New(
				"declared server installation is missing; run lucy install --locked",
			))
		case discovery.FindingOwnershipError:
			output.Warn(fmt.Errorf(
				"cannot verify ownership of %s: %w",
				finding.Path,
				finding.Cause,
			))
		case discovery.FindingModifiedFile:
			output.Warn(fmt.Errorf(
				"managed file %s was modified; it stays unmanaged",
				finding.Path,
			))
		case discovery.FindingReceiptError:
			if finding.Cause != nil {
				output.Warn(fmt.Errorf(
					"cannot verify installation ownership: %w",
					finding.Cause,
				))
			} else {
				output.Warn(errors.New("cannot verify installation ownership"))
			}
		case discovery.FindingInvalidIdentity:
			output.Warn(fmt.Errorf(
				"cannot identify the server in %s: %v",
				finding.Path,
				finding.Cause,
			))
		case discovery.FindingConflictingIdentities,
			discovery.FindingAmbiguousInstallations:
			if resolved || automatic || unresolvedReported {
				continue
			}
			message := unresolvedRuntimeMessage(observed, finding)
			if output.JSON() {
				output.Warn(message)
			} else {
				output.Error(message)
			}
			unresolvedReported = true
		}
	}
}

// unreadableFindingKey identifies one underlying read failure. Recognizers
// can inspect the same artifact, so identical path and cause pairs collapse.
func unreadableFindingKey(finding discovery.Finding) string {
	cause := ""
	if finding.Cause != nil {
		cause = finding.Cause.Error()
	}
	return finding.Path + "\x00" + cause
}

func unreadableFindingError(finding discovery.Finding) error {
	if finding.Cause != nil {
		return fmt.Errorf("cannot read %s: %w", finding.Path, finding.Cause)
	}
	return fmt.Errorf("cannot read %s", finding.Path)
}

// unreadableFindingLines presents each underlying read failure once for a
// command failure detail.
func unreadableFindingLines(result *discovery.Result) []string {
	if result == nil {
		return nil
	}
	var lines []string
	seen := map[string]bool{}
	for _, finding := range result.Diagnostics {
		if finding.Kind != discovery.FindingUnreadableFile {
			continue
		}
		key := unreadableFindingKey(finding)
		if seen[key] {
			continue
		}
		seen[key] = true
		if finding.Cause != nil {
			lines = append(
				lines,
				fmt.Sprintf("%s: %v", finding.Path, finding.Cause),
			)
		} else {
			lines = append(lines, finding.Path)
		}
	}
	return lines
}

func entryPointFindingMessage(finding discovery.Finding) string {
	label := "unmanaged entry point"
	if finding.Kind == discovery.FindingUnmanagedInstallation {
		label = "unmanaged server entry point"
	}
	return fmt.Sprintf("%s %s; left unchanged", label, finding.Path)
}

// automaticSelectionMessage names the chosen entry point and every competing
// entry point. The choice is read-only; the message suggests recording it.
func automaticSelectionMessage(
	observed *discovery.Result,
	finding discovery.Finding,
) error {
	selected := finding.Path
	if selected == "" && observed.Selection.EntryPoint != nil {
		selected = observed.Selection.EntryPoint.Path
	}
	competitors := finding.Paths
	if len(competitors) == 0 {
		competitors = competingEntryPoints(observed, selected)
	}
	message := fmt.Sprintf(
		"multiple server runtimes found; using %s automatically",
		selected,
	)
	if len(competitors) > 0 {
		message += "; competing entry points: " + strings.Join(competitors, ", ")
	}
	message += "; run lucy init --entry-point '<path>' to record a choice"
	return errors.New(message)
}

func unresolvedRuntimeMessage(
	observed *discovery.Result,
	finding discovery.Finding,
) error {
	paths := finding.Paths
	if len(paths) == 0 {
		paths = competingEntryPoints(observed, "")
	}
	if len(paths) == 0 {
		return errors.New("no unambiguous server runtime was found")
	}
	commands := make([]string, 0, len(paths))
	for _, path := range paths {
		commands = append(
			commands,
			"lucy init --entry-point '"+strings.ReplaceAll(path, "'", "'\\''")+"'",
		)
	}
	return fmt.Errorf(
		"cannot choose between server runtimes; select one with %s",
		strings.Join(commands, " or "),
	)
}

// competingEntryPoints is a fallback for findings that carry no paths. It
// lists every entry point except the selected one in a stable order.
func competingEntryPoints(
	result *discovery.Result,
	selected string,
) []string {
	var paths []string
	seen := map[string]bool{}
	for i := range result.Installations {
		for file := range result.Installations[i].EntryPoints() {
			if file.Path == "" || file.Path == selected || seen[file.Path] {
				continue
			}
			seen[file.Path] = true
			paths = append(paths, file.Path)
		}
	}
	slices.Sort(paths)
	return paths
}

// environmentFromLock reads the server identity from the lock's explicit
// fields. The lock always records exact versions, so nothing is inferred.
func environmentFromLock(lock *document.Lock) ServerEnvironment {
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
func ecosystemFromLock(lock *document.Lock) catalog.Ecosystem {
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
		for _, component := range selectedRuntimeComponents(server) {
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
