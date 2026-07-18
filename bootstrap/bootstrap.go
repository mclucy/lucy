package bootstrap

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
)

// Bootstrapper installs a server platform identity package (Minecraft,
// Fabric, Forge, NeoForge, MCDR). These are irreversible, platform-altering
// operations with different failure semantics than regular packages.
type Bootstrapper interface {
	Bootstrap(
		ctx context.Context,
		resolved types.ResolvedPackage,
		serverDir string,
	) error
}

var bootstrappers = map[types.Ecosystem]Bootstrapper{}

// ForEcosystem returns the bootstrapper for the given platform, or an error if none is registered.
func ForEcosystem(platform types.Ecosystem) (Bootstrapper, error) {
	b, ok := bootstrappers[platform]
	if !ok {
		return nil, fmt.Errorf(
			"unsupported platform for bootstrap: %s",
			platform,
		)
	}
	return b, nil
}

func selectedLoader(server *workspace.ServerInstance) types.Ecosystem {
	if server == nil || !server.IsValid() {
		return types.EcoUnspecified
	}
	for _, component := range server.RuntimeComponents {
		switch {
		case component.Eco == types.EcoFabric &&
			(component.Name == "fabric-loader" ||
				component.Name == "fabricloader"):
			return types.EcoFabric
		case component.Eco == types.EcoForge && component.Name == "forge":
			return types.EcoForge
		case component.Eco == types.EcoNeoforge &&
			component.Name == "neoforge":
			return types.EcoNeoforge
		}
	}
	return types.EcoUnspecified
}

func isVanillaServer(server *workspace.ServerInstance) bool {
	return server != nil &&
		server.IsValid() &&
		server.PrimaryRuntime.Identity.Eco == types.EcoMinecraft &&
		server.PrimaryRuntime.Identity.Name == "minecraft"
}
