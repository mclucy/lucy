package routing

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
)

// Candidates routes explicit project identities, never context aliases.
func (r *Router) Candidates(
	ctx context.Context,
	registry catalog.Registry,
	project, minecraft string,
	loader catalog.Ecosystem,
) ([]upstream.Candidate, error) {
	r.logger.Debug(
		"routing: catalogue candidates requested",
		"registry", registry,
		"project", project,
		"minecraft", minecraft,
		"loader", loader,
	)
	catalogue, ok := r.providers.catalogues[registry.Provider()]
	if !ok {
		r.logger.Debug(
			"routing: catalogue registry rejected",
			"registry", registry,
			"reason", "registry has no package catalogue",
		)
		return nil, fmt.Errorf("%w: %q", ErrNoCatalogue, registry)
	}
	return catalogue.Candidates(ctx, project, minecraft, loader)
}
