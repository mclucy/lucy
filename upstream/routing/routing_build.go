package routing

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/providers/curseforge"
	"github.com/mclucy/lucy/upstream/providers/hangar"
	"github.com/mclucy/lucy/upstream/providers/mcdr"
	"github.com/mclucy/lucy/upstream/providers/modrinth"
	"github.com/mclucy/lucy/upstream/providers/spiget"
)

// Builds lists the published versions of an explicitly identified project.
func Builds(ctx context.Context, ref catalog.Ref) ([]upstream.Build, error) {
	project := ref.Name.String()
	switch ref.Provider {
	case catalog.ProviderModrinth:
		return modrinth.Provider.Builds(ctx, project)
	case catalog.ProviderCurseForge:
		return curseforge.Provider.Builds(ctx, project)
	case catalog.ProviderHangar:
		return hangar.Provider.Builds(ctx, project)
	case catalog.ProviderSpiget:
		return spiget.Provider.Builds(ctx, project)
	case catalog.ProviderMCDR:
		return mcdr.Provider.Builds(ctx, project)
	default:
		return nil, fmt.Errorf("%s does not list builds", ref.Provider)
	}
}
