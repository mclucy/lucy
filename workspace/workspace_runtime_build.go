package workspace

import (
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace/internal/detector"
)

func buildServerInstance(
	evidence *detector.ExecutableEvidence,
) *ServerInstance {
	if evidence == nil ||
		evidence.PrimaryRuntime == nil ||
		evidence.PrimaryRuntime.PackageRef == (types.PackageRef{}) ||
		evidence.PrimaryPath == "" {
		return UnknownServer
	}

	return &ServerInstance{
		PrimaryRuntime: evidence.PrimaryRuntime,
		PrimaryPath:    evidence.PrimaryPath,
		RuntimeComponents: normalizeRuntimeComponents(
			evidence.RuntimeComponents,
		),
	}
}
