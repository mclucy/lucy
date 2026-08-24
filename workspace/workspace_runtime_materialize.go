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
		return UnknownExecutable
	}

	return &ServerInstance{
		PrimaryRuntime: &RuntimeArtifact{
			Identity: *evidence.PrimaryRuntime,
			Path:     evidence.PrimaryPath,
		},
		RuntimeComponents: normalizeRuntimeComponents(
			evidence.RuntimeComponents,
		),
	}
}
