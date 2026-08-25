package workspace

import (
	"sort"

	"github.com/mclucy/lucy/types"
)

// PackageIndex is a map-backed package indexing utility that provides
// deterministic, sorted access to a collection of packages. It deduplicates
// packages by their full identifier (PackageId.StringFull()) and guarantees
// that all exported methods return results in a stable, deterministic order.
//
// PackageIndex does NOT expose raw map iteration order to any caller.
type PackageIndex struct {
	pkgs map[string]types.DiscoveredPackage
}

// NewPackageIndex creates a new, empty PackageIndex ready for use.
func NewPackageIndex() *PackageIndex {
	return &PackageIndex{
		pkgs: make(map[string]types.DiscoveredPackage),
	}
}

// Add inserts a package into the index. Dedupe strategy:
//
//   - First-write wins: if a package with the same full ID already exists, the
//     new entry is ignored.
//   - EXCEPTION: if the existing entry has an empty Path (i.e., it was
//     discovered without a local installation path) AND the new package has a
//     non-empty Path, the new package replaces the existing one. This
//     allows adding local path info upon discovery of a package that was
//     previously only known remotely.
//
// The dedupe key is pkg.Id.StringFull(), which encodes platform/name@version.
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

// Packages returns a deterministic sorted projection of all indexed packages.
// The sort order is ascending by:
//
//  1. Platform (string)
//  2. Name (string)
//  3. Version (string)
//
// Unrelated with the map iteration order; results are always sorted.
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

// LookupByID performs an exact lookup by the full package identifier
// (PackageId.StringFull()). Returns the package and true if found, or a zero
// DiscoveredPackage and false otherwise.
func (idx *PackageIndex) LookupByID(id types.VersionedPackageRef) (
	types.DiscoveredPackage,
	bool,
) {
	pkg, ok := idx.pkgs[id.StringFull()]
	return pkg, ok
}

// LookupByEcosystemName returns all packages matching the given platform and
// name, sorted by Version (string ascending). If no packages match, returns
// nil.
//
// Unrelated with the map iteration order; results are always sorted.
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
