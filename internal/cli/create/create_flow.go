// Package create generates a lucy.yaml for an empty workspace.
//
// This package does not handle resolving nor installation.
package create

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/types"
)

// parsedCore is one normalized core request plus its catalog identity.
type parsedCore struct {
	Request types.PackageRequest
	Core    types.CorePackage
}

// parseCores splits a comma- or space-separated core list and parses each
// entry through the shared package syntax. Duplicate entries collapse.
func parseCores(raw string) ([]parsedCore, error) {
	tokens := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	if len(tokens) == 0 {
		// No cores requested. Create records a vanilla server.
		return nil, nil
	}

	cores := make([]parsedCore, 0, len(tokens))
	seen := make(map[string]bool, len(tokens))
	for _, token := range tokens {
		request, err := input.Parse(token)
		if err != nil {
			return nil, fmt.Errorf("parse core %q: %w", token, err)
		}
		key := request.PackageRef.StringBase()
		if seen[key] {
			continue
		}
		seen[key] = true

		core := types.CorePackage(request.Name.String())
		if match, ok := types.NormalizeCorePackage(request); ok {
			core = match.Core
		}
		cores = append(cores, parsedCore{Request: request, Core: core})
	}

	if err := checkPlatformConflict(cores); err != nil {
		return nil, err
	}
	return cores, nil
}

// moddingPlatforms maps manifest platform names to themselves for lookup.
var moddingPlatforms = map[types.CorePackage]bool{
	types.CoreFabric:   true,
	types.CoreForge:    true,
	types.CoreNeoForge: true,
}

// checkPlatformConflict rejects a request that names two different loader
// platforms. A directory can boot only one loader chain.
func checkPlatformConflict(cores []parsedCore) error {
	var first types.CorePackage
	for _, core := range cores {
		if !moddingPlatforms[core.Core] {
			continue
		}
		if first != "" && first != core.Core {
			return fmt.Errorf(
				"cannot combine loader cores %s and %s in one directory",
				first,
				core.Core,
			)
		}
		if first == "" {
			first = core.Core
		}
	}
	return nil
}

// manifestForCores builds the manifest file for a freshly created server.
func manifestForCores(
	gameVersion string,
	cores []parsedCore,
) *state.Manifest {
	mf := state.ManifestDefaults()
	mf.Environment.GameVersion = gameVersion

	for _, core := range cores {
		switch {
		case moddingPlatforms[core.Core]:
			mf.Environment.ModdingPlatform = string(core.Core)
			if isExactVersion(core.Request.Version) {
				mf.Environment.ModdingPlatformVersion = core.Request.Version.String()
			}
		case core.Core == types.CoreMCDReforged:
			mf.Environment.Mcdr = true
		default:
			// Bukkit family and friends record as the bootable core.
			mf.Environment.ServerCore = string(core.Core)
			if isExactVersion(core.Request.Version) {
				mf.Environment.ServerCoreVersion = core.Request.Version.String()
			}
		}
	}

	if mf.Environment.ModdingPlatform == "" && !mf.Environment.Mcdr {
		// Vanilla-only creation still declares a bare platform so the
		// manifest does not read as an accident.
		mf.Environment.ModdingPlatform = "bare"
	}
	return &mf
}

// isExactVersion gates version selectors
func isExactVersion(version types.BareVersion) bool {
	switch version {
	case "", types.VersionNone, types.VersionUnknown, types.VersionAny,
		types.VersionStable, types.VersionBeta:
		return false
	default:
		return true
	}
}
