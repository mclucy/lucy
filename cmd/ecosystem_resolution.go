package cmd

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
		return types.EcoAny, fmt.Errorf("invalid --platform %s", fromFlag)
	}

	if fromQuery == types.EcoAny {
		return platform, nil
	}

	if fromQuery != platform {
		return types.EcoAny, fmt.Errorf(
			"--platform %s conflicts with query prefix %s",
			platform,
			fromQuery,
		)
	}

	return platform, nil
}
