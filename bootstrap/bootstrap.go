package bootstrap

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/types"
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
