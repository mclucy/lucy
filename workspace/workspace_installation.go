package workspace

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/workspace/internal/detector"
)

// RuntimeEntryPoint is a launchable path. Managed requires a matching receipt hash.
type RuntimeEntryPoint struct {
	Path    string `json:"path"`
	Managed bool   `json:"managed"`
}

type RuntimeFindingKind string

const (
	RuntimeAlternativeEntryPoint RuntimeFindingKind = "alternative_entry_point"
	RuntimeUnmanaged             RuntimeFindingKind = "unmanaged_runtime"
)

// RuntimeFinding retains non-selected entry points without treating them as conflicts.
type RuntimeFinding struct {
	Kind    RuntimeFindingKind
	Path    string
	Runtime *ServerRuntime
}

func sameRuntimeIdentity(a, b *detector.RuntimeEvidence) bool {
	if a == nil || b == nil || a.PrimaryRuntime == nil || b.PrimaryRuntime == nil ||
		*a.PrimaryRuntime != *b.PrimaryRuntime || !concreteRuntimeVersion(a.PrimaryRuntime.Version) ||
		len(a.RuntimeComponents) != len(b.RuntimeComponents) {
		return false
	}
	for _, component := range a.RuntimeComponents {
		if !concreteRuntimeVersion(component.Version) || !slices.Contains(b.RuntimeComponents, component) {
			return false
		}
	}
	for _, component := range b.RuntimeComponents {
		if !slices.Contains(a.RuntimeComponents, component) {
			return false
		}
	}
	return true
}

func (p *Probe) groupIdentities() {
	grouped := p.Candidates[:0]
	for _, candidate := range p.Candidates {
		index := slices.IndexFunc(grouped, func(existing IdentifiedRuntime) bool {
			return sameRuntimeIdentity(existing.Evidence, candidate.Evidence)
		})
		if index < 0 {
			grouped = append(grouped, candidate)
			continue
		}
		for _, point := range candidate.EntryPoints {
			if !slices.ContainsFunc(grouped[index].EntryPoints, func(existing RuntimeEntryPoint) bool {
				return existing.Path == point.Path
			}) {
				grouped[index].EntryPoints = append(grouped[index].EntryPoints, point)
			}
		}
	}
	clear(p.Candidates[len(grouped):])
	p.Candidates = grouped
}

// Select chooses a positively identified entry point without discarding other observations.
func (p Probe) Select(path string) (Probe, error) {
	path = filepath.Clean(path)
	for _, candidate := range p.Candidates {
		for _, point := range candidate.EntryPoints {
			if point.Path != path && !sameProbePath(point.Path, path) {
				continue
			}
			runtime := buildServerRuntime(candidate.Evidence)
			if runtime == nil {
				continue
			}
			runtime.PrimaryPath = point.Path
			p.selected, p.selection = true, runtime
			p.updateFindings()
			return p, nil
		}
	}
	return p, fmt.Errorf("--entry-point %q is not an identified server entry point", path)
}

func (p *Probe) updateFindings() {
	p.Findings = nil
	runtime, ok := p.Single()
	if !ok {
		return
	}
	for _, candidate := range p.Candidates {
		candidateRuntime := buildServerRuntime(candidate.Evidence)
		if candidateRuntime == nil {
			continue
		}
		selected := slices.ContainsFunc(candidate.EntryPoints, func(point RuntimeEntryPoint) bool {
			return point.Path == runtime.PrimaryPath
		})
		for _, point := range candidate.EntryPoints {
			if point.Path == runtime.PrimaryPath || point.Managed {
				continue
			}
			kind := RuntimeUnmanaged
			if selected {
				kind = RuntimeAlternativeEntryPoint
			}
			p.Findings = append(p.Findings, RuntimeFinding{
				Kind:    kind,
				Path:    point.Path,
				Runtime: candidateRuntime,
			})
		}
	}
}

func (p Probe) selectionCommands() []string {
	commands := make([]string, 0, len(p.Candidates))
	for _, candidate := range p.Candidates {
		commands = append(commands, "lucy init --entry-point '"+strings.ReplaceAll(candidate.JarPath, "'", "'\\''")+"'")
	}
	return commands
}
