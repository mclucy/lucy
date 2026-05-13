package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/state"
)

// DataSource indicates where dependency data was loaded from.
type DataSource int

const (
	// SourceLock means data was read from .lucy/lock.json.
	SourceLock DataSource = iota

	// SourceProbe means data was obtained by probing the live server directory.
	SourceProbe
)

// String returns a human-readable label for the data source.
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

// LoadDependencyData loads the dependency graph from the best available source.
// When forceLive is true, the lock file is skipped and the server directory is
// probed directly. Otherwise the lock file is tried first; if it is missing or
// invalid the directory is probed as a fallback.
func LoadDependencyData(workDir string, forceLive bool) (*DependencyGraph, DataSource, error) {
	if !forceLive {
		lockPath := filepath.Join(workDir, ".lucy", "lock.json")
		data, err := os.ReadFile(lockPath)
		if err == nil {
			var lock state.Lock
			if err := json.Unmarshal(data, &lock); err == nil {
				graph, err := BuildGraphFromLock(lock)
				if err != nil {
					return nil, 0, fmt.Errorf("failed to build graph from lock: %w", err)
				}
				return graph, SourceLock, nil
			}
			// Invalid lock JSON — fall through to probe.
		}
		// Lock file missing — fall through to probe.
	}

	info := probe.ServerInfoAt(workDir)
	graph, err := BuildGraphFromProbe(info)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to build graph from probe: %w", err)
	}
	return graph, SourceProbe, nil
}
