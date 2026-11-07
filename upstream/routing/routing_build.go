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
	catalogue, ok := r.providers.catalogues[ref.Provider]
	if !ok {
		if _, repository := ref.Provider.Registry(); repository {
			return nil, fmt.Errorf(
				"%w: %s",
				ErrUnsupportedProvider,
				ref.Provider,
			)
		}
		return nil, fmt.Errorf("%s does not list builds", ref.Provider)
	}
	return catalogue.Builds(ctx, ref.Name.String())
}
