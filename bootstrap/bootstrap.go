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

// selectedLoader reports the loader ecosystem the detected runtime already
// serves, including hybrids (CatServer, Youer) whose loader shows up only as
// an effective ecosystem offer rather than a runtime component. Degraded
// offers (bridge mods) do not claim the loader slot.
func selectedLoader(server *workspace.ServerInstance) types.Ecosystem {
	if server == nil || !server.IsValid() {
		return types.EcoUnspecified
	}
	for _, offer := range server.EffectiveEcosystems() {
		if offer.Compatibility == types.CompatCompatible &&
			offer.Ecosystem.IsModding() {
			return offer.Ecosystem
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
