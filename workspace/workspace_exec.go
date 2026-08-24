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

// getExecutableInfo uses the new detector-based architecture to find server executables
func buildExecutableInfo() *ServerInstance {
	valid := make([]*detector.ExecutableEvidence, 0)
	workPath := workPath()
	// scanned counts the candidate executables that we analyzed.
	scanned := 0
	// contradicted is true when more than one specific detector matched one jar.
	contradicted := false
	// ambiguous holds the paths of the jars that caused a contradiction.
	ambiguous := []string{}

	// scanArtifact analyzes one candidate jar:
	// - no match: scanned increases
	// - one match: the match goes into valid
	// - more than one specific match: contradicted becomes true and the jar
	//   path goes into ambiguous
	scanArtifact := func(jar string) {
		scanned++
		candidates := detector.Executable(jar)
		if candidates == nil || candidates.IsEmpty() {
			return
		}
		if candidates.IsAmbiguous() {
			contradicted = true
			ambiguous = append(ambiguous, jar)
			return
		}
		valid = append(valid, candidates.Single())
	}
	for _, evidence := range detector.ForgeInstallationRuntimes(workPath) {
		valid = append(valid, evidence)
	}
	for _, evidence := range detector.NeoForgeInstallationRuntimes(workPath) {
		valid = append(valid, evidence)
	}

	// Scan the root directory.
	jars, err := findJar(workPath)
	if err != nil {
		log.Warn(fmt.Errorf("cannot read server directory: %w", err))
	}
	for _, jar := range jars {
		scanArtifact(jar)
	}

	switch {
	case len(valid) == 1 && !contradicted:
		return buildServerInstance(valid[0])
	case len(valid) == 0 && scanned == 0:
		log.Info("no server executable found")
		return NoServer
	case len(valid) == 0:
		log.Info(fmt.Sprintf(
			"%d candidate executables examined, none identifiable as a server",
			scanned,
		))
		return UnknownServer
	default:
		if contradicted {
			log.ReportError(fmt.Errorf(
				"found conflicting server files in %s (%s); lucy cannot tell which server this directory runs",
				workPath,
				strings.Join(ambiguous, ", "),
			))
		} else {
			log.ReportError(fmt.Errorf(
				"found multiple servers side by side in %s: %s; lucy cannot tell which one to use",
				workPath,
				strings.Join(evidenceLabels(valid), "; "),
			))
		}
		return UnknownServer
	}
}

var getExecutableInfo = fn.Memoize(buildExecutableInfo)

func init() {
	resetProbeExecCache = func() {
		getExecutableInfo = fn.Memoize(buildExecutableInfo)
	}
}

// evidenceLabels renders one line of text for each detected server. The text
// shows the package ref and the file name.
func evidenceLabels(evidence []*detector.ExecutableEvidence) []string {
	labels := make([]string, 0, len(evidence))
	for _, e := range evidence {
		if e == nil || e.PrimaryRuntime == nil {
			continue
		}
		labels = append(labels, fmt.Sprintf(
			"%s/%s@%s (%s)",
			e.PrimaryRuntime.Eco,
			e.PrimaryRuntime.Name,
			e.PrimaryRuntime.Version,
			filepath.Base(e.PrimaryPath),
		))
	}
	return labels
}

func findJar(dir ...string) (jarFiles []string, err error) {
	jarFiles = []string{}
	for _, d := range dir {
		files, err := findFileWithExt(d, ".jar")
		if err != nil {
			return nil, err
		}
		jarFiles = append(jarFiles, files...)
	}
	return jarFiles, nil
}

func findFileWithExt(dir string, ext ...string) (files []string, err error) {
	files = []string{}
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

	return files, nil
}
