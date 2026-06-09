package cmd

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/types"
)

func ResolvePlatform(
	fromQuery types.PlatformId,
	fromFlag string,
) (types.PlatformId, error) {
	if fromFlag == "" {
		return fromQuery, nil
	}

	platform := types.PlatformId(strings.ToLower(strings.TrimSpace(fromFlag)))
	if !platform.IsSearchPlatform() {
		return types.PlatformAny, fmt.Errorf("invalid --platform %s", fromFlag)
	}

	if fromQuery == types.PlatformAny {
		return platform, nil
	}

	if fromQuery != platform {
		return types.PlatformAny, fmt.Errorf(
			"--platform %s conflicts with query prefix %s",
			platform,
			fromQuery,
		)
	}

	return platform, nil
}
