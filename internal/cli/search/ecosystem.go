package search

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/types"
)

func ResolveEcosystem(
	fromQuery types.Ecosystem,
	fromFlag string,
) (types.Ecosystem, error) {
	if fromFlag == "" {
		return fromQuery, nil
	}

	platform := types.Ecosystem(strings.ToLower(strings.TrimSpace(fromFlag)))
	if !platform.IsSearchEcosystem() {
		return types.EcoUnspecified, fmt.Errorf(
			"invalid --platform %s",
			fromFlag,
		)
	}

	if fromQuery == types.EcoUnspecified {
		return platform, nil
	}

	if fromQuery != platform {
		return types.EcoUnspecified, fmt.Errorf(
			"--platform %s conflicts with query prefix %s",
			platform,
			fromQuery,
		)
	}

	return platform, nil
}
