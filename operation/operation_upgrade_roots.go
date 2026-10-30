package operation

import (
	"bytes"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/resolve"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

type upgradeRoot struct {
	Environment serverpkg.Environment
	Ecosystem   catalog.Ecosystem
	Reference   string
	Requirement document.Requirement
	Provider    catalog.Provider
	Project     string
	Previous    *document.Package
	Versions    []upgradeVersion
	LockedIndex int
	LockedRank  int
}

type upgradeVersion struct {
	Version     versionpkg.Version
	Builds      []upstream.Candidate
	Rank        int
	PublishedAt time.Time
}

// PlanUpgrade resolves candidate frontiers without staging or committing.

func upgradeFloating(v versionpkg.Version) bool {
	switch v {
	case "", versionpkg.Any, versionpkg.Beta, versionpkg.Stable, "latest":
		return true
	default:
		return false
	}
}

func upgradeRoots(d *document.Manifest, old *document.Lock) ([]upgradeRoot, error) {
	var roots []upgradeRoot
	for _, ecosystem := range sortedEcosystems(d.Server.Packages) {
		for ref, requirement := range d.Server.Packages[ecosystem] {
			if !requirement.IsEnabled() {
				continue
			}
			root, err := newUpgradeRoot(serverpkg.EnvironmentServer, ecosystem, ref, requirement, old)
			if err != nil {
				return nil, err
			}
			roots = append(roots, root)
		}
	}
	if d.MCDR != nil {
		for ref, requirement := range d.MCDR.Packages {
			if !requirement.IsEnabled() {
				continue
			}
			root, err := newUpgradeRoot(serverpkg.EnvironmentMCDR, catalog.EcosystemMCDR, ref, requirement, old)
			if err != nil {
				return nil, err
			}
			roots = append(roots, root)
		}
	}
	sort.Slice(roots, func(i, j int) bool {
		if n := strings.Compare(string(roots[i].Environment), string(roots[j].Environment)); n != 0 {
			return n < 0
		}
		if n := strings.Compare(string(roots[i].Ecosystem), string(roots[j].Ecosystem)); n != 0 {
			return n < 0
		}
		return roots[i].Reference < roots[j].Reference
	})
	return roots, nil
}

func newUpgradeRoot(
	environment serverpkg.Environment,
	ecosystem catalog.Ecosystem,
	ref string,
	requirement document.Requirement,
	old *document.Lock,
) (upgradeRoot, error) {
	provider, project, err := document.ParseReference(ref)
	if err != nil {
		return upgradeRoot{}, err
	}
	previous := previousRootPackage(old, environment, ecosystem, ref, provider, project)
	if previous == nil {
		return upgradeRoot{}, fmt.Errorf("lock has no package for manifest root %s; run lucy install first", ref)
	}
	return upgradeRoot{
		Environment: environment,
		Ecosystem:   ecosystem,
		Reference:   ref,
		Requirement: requirement,
		Provider:    provider,
		Project:     project,
		Previous:    previous,
	}, nil
}

func previousRootPackage(
	old *document.Lock,
	environment serverpkg.Environment,
	ecosystem catalog.Ecosystem,
	ref string,
	provider catalog.Provider,
	project string,
) *document.Package {
	for i := range old.Packages {
		p := &old.Packages[i]
		if p.Environment != environment || p.Ecosystem != ecosystem {
			continue
		}
		for _, binding := range p.Requirements {
			if binding.Reference == ref {
				return p
			}
		}
	}
	for i := range old.Packages {
		p := &old.Packages[i]
		if p.Environment == environment && p.Ecosystem == ecosystem &&
			p.Provider == provider && p.ProjectID == project {
			return p
		}
	}
	return nil
}

func upgradeTargets(roots []upgradeRoot, opts UpgradeOptions) (map[string]bool, error) {
	if len(opts.Targets) == 0 {
		targets := make(map[string]bool, len(roots))
		for _, root := range roots {
			targets[root.Reference] = true
		}
		return targets, nil
	}
	targets := map[string]bool{}
	for _, raw := range opts.Targets {
		request, err := input.Parse(raw)
		if err != nil {
			return nil, err
		}
		if request.Version != "" && request.Version != versionpkg.Any {
			return nil, fmt.Errorf("upgrade targets name packages, not versions: %s", raw)
		}
		var matched []string
		for _, root := range roots {
			if request.Provider != catalog.ProviderUnspecified && request.Provider != root.Provider {
				continue
			}
			if !strings.EqualFold(string(request.Name), root.Project) {
				continue
			}
			matched = append(matched, root.Reference)
		}
		switch len(matched) {
		case 0:
			return nil, fmt.Errorf("package %s is not an enabled manifest root", raw)
		case 1:
			targets[matched[0]] = true
		default:
			return nil, fmt.Errorf("package %s is ambiguous: %s", raw, strings.Join(matched, ", "))
		}
	}
	return targets, nil
}

func pinCoreAndMCDR(d *document.Manifest, old *document.Lock) {
	d.Server.Minecraft = old.Server.Minecraft
	d.Server.Core.Product = old.Server.Product
	d.Server.Core.ProductVersion = old.Server.CoreVersion
	if d.MCDR != nil && old.MCDR != nil {
		d.MCDR.Version = old.MCDR.Version
	}
}

func upgradeTarget(old *document.Lock, coreModules []catalog.DescriptorModule) resolve.Target {
	mcdrVersion := ""
	if old.MCDR != nil {
		mcdrVersion = string(old.MCDR.Version)
	}
	return resolve.Target{
		Product:      old.Server.Product,
		Bootstrapper: old.Server.Bootstrapper,
		Minecraft:    old.Server.Minecraft,
		CoreVersion:  old.Server.CoreVersion,
		MCDRVersion:  mcdrVersion,
		CoreModules:  coreModules,
	}
}

func rootRequirements(d *document.Manifest) map[string]document.Requirement {
	requirements := map[string]document.Requirement{}
	for _, ecosystem := range sortedEcosystems(d.Server.Packages) {
		for ref, requirement := range d.Server.Packages[ecosystem] {
			requirements[ref] = requirement
		}
	}
	if d.MCDR != nil {
		for ref, requirement := range d.MCDR.Packages {
			requirements[ref] = requirement
		}
	}
	return requirements
}

func setRootSelector(d *document.Manifest, root upgradeRoot, selector versionpkg.Version) error {
	if root.Environment == serverpkg.EnvironmentMCDR {
		if d.MCDR == nil {
			return fmt.Errorf("manifest has no MCDR runtime")
		}
		requirement, ok := d.MCDR.Packages[root.Reference]
		if !ok {
			return fmt.Errorf("manifest root %s is missing", root.Reference)
		}
		requirement.Selector = selector
		d.MCDR.Packages[root.Reference] = requirement
		return nil
	}
	packages, ok := d.Server.Packages[root.Ecosystem]
	if !ok {
		return fmt.Errorf("manifest ecosystem %s is missing", root.Ecosystem)
	}
	requirement, ok := packages[root.Reference]
	if !ok {
		return fmt.Errorf("manifest root %s is missing", root.Reference)
	}
	requirement.Selector = selector
	packages[root.Reference] = requirement
	return nil
}

func cloneManifest(d *document.Manifest) (*document.Manifest, error) {
	data, err := d.CanonicalBytes()
	if err != nil {
		return nil, err
	}
	return document.DecodeManifest(bytes.NewReader(data))
}

func sortedEcosystems(packages map[catalog.Ecosystem]map[string]document.Requirement) []catalog.Ecosystem {
	ecosystems := make([]catalog.Ecosystem, 0, len(packages))
	for ecosystem := range packages {
		ecosystems = append(ecosystems, ecosystem)
	}
	slices.Sort(ecosystems)
	return ecosystems
}
