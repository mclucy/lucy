package routing

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/providers/curseforge"
	"github.com/mclucy/lucy/upstream/providers/mcdr"
	"github.com/mclucy/lucy/upstream/providers/modrinth"
)

// Candidates routes explicit project identities, never context aliases.
func Candidates(ctx context.Context, provider, project, minecraft string, loader types.Ecosystem) ([]types.CatalogueCandidate, error) {
	switch provider {
	case "modrinth":
		return modrinth.Provider.Candidates(ctx, project, minecraft, loader)
	case "curseforge":
		return curseforge.Provider.Candidates(ctx, project, minecraft, loader)
	case "mcdr":
		return mcdr.Provider.Candidates(ctx, project, minecraft, loader)
	default:
		return nil, fmt.Errorf("unsupported package catalogue %q", provider)
	}
}
