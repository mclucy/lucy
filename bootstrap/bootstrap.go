package bootstrap

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
)

// Bootstrapper installs a server ecosystem identity package (Minecraft,
// Fabric, Forge, NeoForge, MCDR). These are irreversible, ecosystem-altering
// operations with different failure semantics than regular packages.
type Bootstrapper interface {
	Bootstrap(
		ctx context.Context,
		resolved types.ResolvedPackage,
		serverDir string,
	) error
}

var bootstrappers = map[types.Ecosystem]Bootstrapper{}

// ForEcosystem returns the bootstrapper for the given ecosystem, or an error if none is registered.
func ForEcosystem(ecosystem types.Ecosystem) (Bootstrapper, error) {
	b, ok := bootstrappers[ecosystem]
	if !ok {
		return nil, fmt.Errorf(
			"unsupported ecosystem for bootstrap: %s",
			ecosystem,
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
		server.PrimaryRuntime.Eco == types.EcoMinecraft &&
		server.PrimaryRuntime.Name == "minecraft"
}
