package routing

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/providers/curseforge"
	"github.com/mclucy/lucy/upstream/providers/mcdr"
	"github.com/mclucy/lucy/upstream/providers/modrinth"
)

// Candidates routes explicit project identities, never context aliases.
func Candidates(
	ctx context.Context,
	registry types.Registry,
	project, minecraft string,
	loader types.Ecosystem,
) ([]types.CatalogueCandidate, error) {
	log.Debug(
		"routing: catalogue candidates requested",
		"registry", registry,
		"project", project,
		"minecraft", minecraft,
		"loader", loader,
	)
	switch registry {
	case types.RegistryModrinth:
		return modrinth.Provider.Candidates(ctx, project, minecraft, loader)
	case types.RegistryCurseForge:
		return curseforge.Provider.Candidates(ctx, project, minecraft, loader)
	case types.RegistryMCDR:
		return mcdr.Provider.Candidates(ctx, project, minecraft, loader)
	default:
		log.Debug(
			"routing: catalogue registry rejected",
			"registry", registry,
			"reason", "registry has no package catalogue",
		)
		return nil, fmt.Errorf("%w: %q", ErrNoCatalogue, registry)
	}
}
