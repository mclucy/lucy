package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/workspace/internal/detector"
)

// RuntimeCandidate is one examined artifact. Exactly one specific detector
// claims it as a bootable server runtime. Evidence carries the complete
// identity from that detector. The scan does not change the evidence after
// it returns.
type RuntimeCandidate struct {
	JarPath  string
	Evidence *detector.ExecutableEvidence
}

// AmbiguousRuntimeCandidate is one artifact that more than one specific detector
// claims. The scan cannot decide between the claims. The scan keeps all
// claims and keeps the jar out of Single.
type AmbiguousRuntimeCandidate struct {
	JarPath  string
	Evidence []*detector.ExecutableEvidence
}

// Probe holds the results of one directory scan. Each examined artifact
// appears in exactly one bucket:
//
//   - Candidates: artifacts that exactly one detector identifies as a
//     bootable runtime
//   - AmbiguousCandidates: artifacts with contradictory identity claims
//   - Unidentified: artifacts that no detector identifies
//
// Probe reports observations. The methods on Probe interpret them.
type Probe struct {
	Candidates          []RuntimeCandidate
	AmbiguousCandidates []AmbiguousRuntimeCandidate
	Unidentified        []string
}

// Single interprets the probe as exactly one trustworthy server runtime.
// Single fails when the scan saw contradictory evidence, several runtimes,
// or no usable runtime.
func (p Probe) Single() (*ServerInstance, bool) {
	if len(p.Candidates) != 1 || len(p.AmbiguousCandidates) > 0 {
		return nil, false
	}
	server := buildServerInstance(p.Candidates[0].Evidence)
	return server, server != nil
}

// HasAmbiguity reports ambiguity in the directory contents. Ambiguity means
// one of these conditions:
//   - two or more detectors claim one jar
//   - two or more distinct runtimes share one directory
func (p Probe) HasAmbiguity() bool {
	return len(p.AmbiguousCandidates) > 0 || len(p.Candidates) > 1
}

// IsEmpty reports whether the scan found no examinable artifact.
func (p Probe) IsEmpty() bool {
	return len(p.Candidates) == 0 &&
		len(p.AmbiguousCandidates) == 0 &&
		len(p.Unidentified) == 0
}

// probeDirectory scans root and sorts each piece of runtime evidence into
// its bucket. It writes user-facing diagnostics once per observation. The
// interpretation methods then stay quiet and repeatable.
func probeDirectory(root string) Probe {
	var p Probe

	// Installer layouts (Forge, NeoForge) produce runtime evidence from the
	// libraries tree. They do not need a scannable root jar. They take part
	// in ambiguity detection like every other candidate.
	for _, evidence := range detector.ForgeInstallationRuntimes(root) {
		p.addCandidate(evidence.PrimaryPath, evidence)
	}
	for _, evidence := range detector.NeoForgeInstallationRuntimes(root) {
		p.addCandidate(evidence.PrimaryPath, evidence)
	}

	context, err := detector.NewDetectionContext(root)
	if err != nil {
		log.Warn(fmt.Errorf("cannot read server directory: %w", err))
		return p
	}
	files := context.RootFiles()
	for _, file := range files {
		if file.IsDirectory() || !strings.HasSuffix(strings.ToLower(file.Path()), ".jar") {
			continue
		}
		claims := detector.Executable(context, file)
		switch {
		case claims == nil || claims.IsEmpty():
			p.Unidentified = append(p.Unidentified, file.Path())
		case claims.IsAmbiguous():
			p.AmbiguousCandidates = append(p.AmbiguousCandidates, AmbiguousRuntimeCandidate{
				JarPath:  file.Path(),
				Evidence: claims.Candidates,
			})
		default:
			p.addCandidate(file.Path(), claims.Single())
		}
	}

	p.foldConsumedComponents()
	reportScanDiagnostics(root, p, len(files))
	return p
}

func (p *Probe) addCandidate(jarPath string, evidence *detector.ExecutableEvidence) {
	p.Candidates = append(p.Candidates, RuntimeCandidate{
		JarPath:  jarPath,
		Evidence: evidence,
	})
}

// foldConsumedComponents drops vanilla runtime candidates that another
// candidate consumes as a component. A Fabric launch shim boots the jar named
// by its serverJar setting; the shim and that jar are one runtime, not two
// servers side by side.
func (p *Probe) foldConsumedComponents() {
	consumed := make(map[string]bool)
	for _, candidate := range p.Candidates {
		for _, file := range candidate.Evidence.ConsumedFiles {
			if file != nil {
				consumed[file.Path()] = true
			}
		}
	}
	if len(consumed) == 0 {
		return
	}

	kept := make([]RuntimeCandidate, 0, len(p.Candidates))
	for _, candidate := range p.Candidates {
		if candidate.Evidence.IsVanilla() && consumed[candidate.Evidence.PrimaryPath] {
			continue
		}
		kept = append(kept, candidate)
	}
	p.Candidates = kept
}

// reportScanDiagnostics writes the messages that a person needs for an
// ambiguous or empty directory. It runs once per scan. Readers of Single
// never repeat these messages.
func reportScanDiagnostics(root string, p Probe, examined int) {
	switch {
	case p.IsEmpty():
		log.Info("no server executable found")
	case len(p.Candidates) == 0 && len(p.AmbiguousCandidates) == 0:
		log.Info(fmt.Sprintf(
			"%d candidate executables examined, none identifiable as a server",
			examined,
		))
	}

	if conflicted := conflictLabels(p.AmbiguousCandidates); len(conflicted) > 0 {
		log.ReportError(fmt.Errorf(
			"found conflicting server files in %s (%s); lucy cannot tell which server this directory runs",
			root,
			strings.Join(conflicted, ", "),
		))
	}
	if len(p.Candidates) > 1 {
		log.ReportError(fmt.Errorf(
			"found multiple servers side by side in %s: %s; lucy cannot tell which one to use",
			root,
			strings.Join(candidateLabels(p.Candidates), "; "),
		))
	}
}

// conflictLabels writes one entry for each jar whose detectors disagree.
func conflictLabels(jars []AmbiguousRuntimeCandidate) []string {
	labels := make([]string, 0, len(jars))
	for _, jar := range jars {
		labels = append(labels, jar.JarPath)
	}
	return labels
}

// candidateLabels writes one entry for each identified runtime. The entry
// shows the claimed package ref and the file name.
func candidateLabels(candidates []RuntimeCandidate) []string {
	labels := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		evidence := candidate.Evidence
		if evidence == nil || evidence.PrimaryRuntime == nil {
			continue
		}
		labels = append(labels, fmt.Sprintf(
			"%s/%s@%s (%s)",
			evidence.PrimaryRuntime.Eco,
			evidence.PrimaryRuntime.Name,
			evidence.PrimaryRuntime.Version,
			filepath.Base(evidence.PrimaryPath),
		))
	}
	return labels
}

// findJar lists regular files with a .jar extension directly inside each
// given directory. It does not walk subdirectories. Servers keep their
// runtime jars at shallow locations with known names.
func findJar(dir ...string) ([]string, error) {
	return findFileWithExt(dir, ".jar")
}

func findFileWithExt(dirs []string, ext ...string) ([]string, error) {
	files := []string{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if fn.Exists(ext, filepath.Ext(entry.Name())) {
				files = append(files, filepath.Join(dir, entry.Name()))
			}
		}
	}
	return files, nil
}
