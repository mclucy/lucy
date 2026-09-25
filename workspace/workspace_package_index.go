package workspace

import (
	"sort"

	"github.com/mclucy/lucy/types"
)

// PackageIndex is a map-backed index over discovered packages, keyed by the
// full identifier PackageId.StringFull(). All exported methods return
// results in a stable order; raw map iteration order never reaches callers.
type PackageIndex struct {
	pkgs map[string]types.DiscoveredPackage
}

func NewPackageIndex() *PackageIndex {
	return &PackageIndex{
		pkgs: make(map[string]types.DiscoveredPackage),
	}
}

// Add inserts a package into the index. First-write wins, except that a
// non-empty Path replaces an existing entry whose Path is empty, so an
// entry without a local path can gain one when discovered.
func (idx *PackageIndex) Add(pkg types.DiscoveredPackage) {
	key := pkg.Id.StringFull()

	existing, exists := idx.pkgs[key]
	if exists {
		if existing.Path != "" || pkg.Path == "" {
			return
		}
	}

	idx.pkgs[key] = pkg
}

// Merge bulk-adds a slice of packages into the index. Each package is subject
// to the same dedupe policy as Add.
func (idx *PackageIndex) Merge(pkgs []types.DiscoveredPackage) {
	for _, pkg := range pkgs {
		idx.Add(pkg)
	}
}

// Packages returns all indexed packages sorted ascending by platform,
// name, and version string.
func (idx *PackageIndex) Packages() []types.DiscoveredPackage {
	result := make([]types.DiscoveredPackage, 0, len(idx.pkgs))
	for _, pkg := range idx.pkgs {
		result = append(result, pkg)
	}

	sort.Slice(
		result, func(i, j int) bool {
			pi, pj := result[i].Id, result[j].Id

			if pi.Eco != pj.Eco {
				return pi.Eco.String() < pj.Eco.String()
			}
			if pi.Name != pj.Name {
				return pi.Name.String() < pj.Name.String()
			}
			return pi.Version.String() < pj.Version.String()
		},
	)

	return result
}

// LookupByID performs an exact lookup by the full identifier
// (PackageId.StringFull()), returning a zero package and false when absent.
func (idx *PackageIndex) LookupByID(id types.VersionedPackageRef) (
	types.DiscoveredPackage,
	bool,
) {
	pkg, ok := idx.pkgs[id.StringFull()]
	return pkg, ok
}

// LookupByEcosystemName returns all packages matching the given platform
// and name, sorted by version string ascending, or nil when none match.
func (idx *PackageIndex) LookupByEcosystemName(
	platform types.Ecosystem,
	name string,
) []types.DiscoveredPackage {
	var matches []types.DiscoveredPackage
	for _, pkg := range idx.pkgs {
		if pkg.Id.Eco == platform && pkg.Id.Name.String() == name {
			matches = append(matches, pkg)
		}
	}

	if len(matches) == 0 {
		return nil
	}

	sort.Slice(
		matches, func(i, j int) bool {
			return matches[i].Id.Version.String() < matches[j].Id.Version.String()
		},
	)

	return matches
}
