package routing

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
)

// Builds lists the published versions of an explicitly identified project.
func (r *Router) Builds(
	ctx context.Context,
	ref catalog.Ref,
) ([]upstream.Build, error) {
	project := ref.Name.String()
	switch ref.Provider {
	case catalog.ProviderModrinth:
		return r.modrinth.Builds(ctx, project)
	case catalog.ProviderCurseForge:
		if r.curseforge == nil {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, ref.Provider)
		}
		return r.curseforge.Builds(ctx, project)
	case catalog.ProviderHangar:
		return r.hangar.Builds(ctx, project)
	case catalog.ProviderSpiget:
		return r.spiget.Builds(ctx, project)
	case catalog.ProviderMCDR:
		return r.mcdr.Builds(ctx, project)
	default:
		return nil, fmt.Errorf("%s does not list builds", ref.Provider)
	}
}
