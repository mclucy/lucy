package install

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mclucy/lucy/catalog"
	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/version"
)

const mcdrPyPIURL = "https://pypi.org/pypi/mcdreforged/json"

// resolveMCDRVersion picks the MCDReforged version recorded in the lock. A
// floating selector keeps the locked version; otherwise the latest stable
// release comes from PyPI metadata, without touching the local Python.
func resolveMCDRVersion(
	ctx context.Context,
	request version.Version,
	previous *documentpkg.Lock,
	offline bool,
) (version.Version, error) {
	if !isFloating(request) {
		return request, nil
	}
	if previous != nil && previous.MCDR != nil {
		return previous.MCDR.Version, nil
	}
	if offline {
		return "", fmt.Errorf(
			"MCDReforged version %q needs network metadata; run online or pin mcdr.version",
			request,
		)
	}
	return latestMCDRVersion(ctx)
}

func latestMCDRVersion(ctx context.Context) (version.Version, error) {
	var document struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := routing.Metadata(ctx, mcdrPyPIURL, &document); err != nil {
		return "", fmt.Errorf("look up MCDReforged releases: %w", err)
	}
	raw := strings.TrimSpace(document.Info.Version)
	if raw == "" {
		return "", fmt.Errorf("MCDReforged releases carry no version")
	}
	engine, err := version.NewEngine(version.DialectMCDR)
	if err != nil {
		return "", err
	}
	if _, err := engine.ParseVersion(version.Version(raw)); err != nil {
		return "", fmt.Errorf("MCDReforged release version %q is not usable: %w", raw, err)
	}
	return version.Version(raw), nil
}

// mcdrPythonRequirements returns the pip requirements that the locked MCDR
// package set needs. The interpreter resolves and downloads wheels on the
// machine that runs install, so no Python version or wheel is recorded in the
// portable lock. The isolated environment is keyed by this list, so a changed
// list provisions a fresh environment.
func mcdrPythonRequirements(d *documentpkg.Lock) []string {
	if d == nil || d.MCDR == nil {
		return nil
	}
	requirements := []string{"mcdreforged==" + string(d.MCDR.Version)}
	for _, p := range d.Packages {
		for _, m := range p.Modules {
			for _, dependency := range m.Dependencies {
				if dependency.Kind != catalog.DependencyKindPython {
					continue
				}
				id := strings.TrimSpace(dependency.ID)
				if id == "" || id == "mcdreforged" {
					continue
				}
				requirements = append(requirements, id)
			}
		}
	}
	slices.Sort(requirements)
	return slices.Compact(requirements)
}
