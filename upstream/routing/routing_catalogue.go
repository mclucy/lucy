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
func Candidates(ctx context.Context, provider, project, minecraft string, loader types.Ecosystem) ([]types.CatalogueCandidate, error) {
	log.Debug(
		"routing: catalogue candidates requested",
		"provider", provider,
		"project", project,
		"minecraft", minecraft,
		"loader", loader,
	)
	switch provider {
	case "modrinth":
		return modrinth.Provider.Candidates(ctx, project, minecraft, loader)
	case "curseforge":
		return curseforge.Provider.Candidates(ctx, project, minecraft, loader)
	case "mcdr":
		return mcdr.Provider.Candidates(ctx, project, minecraft, loader)
	default:
		log.Debug(
			"routing: catalogue provider rejected",
			"provider", provider,
			"reason", "source has no package catalogue",
		)
		return nil, fmt.Errorf("%w: %q", ErrNoCatalogue, provider)
	}
}
