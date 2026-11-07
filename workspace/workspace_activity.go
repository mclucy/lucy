package workspace

import (
	"github.com/mclucy/lucy/workspace/discovery"
)

// RunningAt checks the native world session lock without catalogue or app lookup.
func RunningAt(anchor string) bool {
	root := readMCDRWorkingDirectory(anchor)
	ws := &Workspace{ServerDir: root}
	ws.Discovery = discovery.Discover(root)
	ws.selectionErr = ws.Discovery.Select(discovery.Request{})
	return ws.Running()
}
