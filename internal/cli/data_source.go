package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/state"
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

// LoadDependencyData builds the dependency graph from the lock file, falling
// back to a live probe when the lock is missing or invalid. forceLive skips
// the lock file entirely.
func LoadDependencyData(workDir string, forceLive bool) (
	*DependencyGraph,
	DataSource,
	error,
) {
	if !forceLive {
		lockPath := filepath.Join(workDir, string(state.LockFile))
		data, err := os.ReadFile(lockPath)
		if err == nil {
			var lock state.Lock
			if err := lock.Unmarshal(data); err == nil {
				graph, err := BuildGraphFromLock(lock)
				if err != nil {
					return nil, 0, fmt.Errorf(
						"failed to build graph from lock: %w",
						err,
					)
				}
				return graph, SourceLock, nil
			}
			// Invalid lock file — fall through to probe.
		}
		// Lock file missing — fall through to probe.
	}

	info := workspace.NewAt(workDir)
	graph, err := BuildGraphFromProbe(info)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to build graph from probe: %w", err)
	}
	return graph, SourceProbe, nil
}
