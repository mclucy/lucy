package probe

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
	pkgs map[string]types.Package
}

// NewPackageIndex creates a new, empty PackageIndex ready for use.
func NewPackageIndex() *PackageIndex {
	return &PackageIndex{
		pkgs: make(map[string]types.Package),
	}
}

// Add inserts a package into the index with the following dedupe policy:
//
//   - First-write wins: if a package with the same full ID already exists, the
//     new entry is ignored.
//   - EXCEPTION: if the existing entry has an empty Local.Path (i.e., it was
//     discovered without a local installation path) AND the new package has a
//     non-empty Local.Path, the new package replaces the existing one. This
//     allows local-path enrichment to take precedence over remote-only entries.
//
// The dedupe key is pkg.Id.StringFull(), which encodes platform/name@version.
func (idx *PackageIndex) Add(pkg types.Package) {
	key := pkg.Id.StringFull()

	existing, exists := idx.pkgs[key]
	if exists {
		// First-write wins, UNLESS the existing entry lacks a local path
		// and the incoming package provides one — in that case, the new
		// entry replaces the old so that local-path information is preserved.
		existingPath := ""
		if existing.Local != nil {
			existingPath = existing.Local.Path
		}
		newPath := ""
		if pkg.Local != nil {
			newPath = pkg.Local.Path
		}

		if existingPath != "" || newPath == "" {
			return
		}
	}

	idx.pkgs[key] = pkg
}

// Merge bulk-adds a slice of packages into the index. Each package is subject
// to the same dedupe policy as Add.
func (idx *PackageIndex) Merge(pkgs []types.Package) {
	for _, pkg := range pkgs {
		idx.Add(pkg)
	}
}

// Packages returns a deterministic sorted projection of all indexed packages.
// The sort order is ascending by:
//  1. Platform (string)
//  2. Name (string)
//  3. Version (string)
//
// This method never exposes map iteration order; results are always sorted.
func (idx *PackageIndex) Packages() []types.Package {
	result := make([]types.Package, 0, len(idx.pkgs))
	for _, pkg := range idx.pkgs {
		result = append(result, pkg)
	}

	sort.Slice(
		result, func(i, j int) bool {
			pi, pj := result[i].Id, result[j].Id

			if pi.Platform != pj.Platform {
				return pi.Platform.String() < pj.Platform.String()
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
// Package and false otherwise.
func (idx *PackageIndex) LookupByID(id types.VersionedPackageRef) (
	types.Package,
	bool,
) {
	pkg, ok := idx.pkgs[id.StringFull()]
	return pkg, ok
}

// LookupByPlatformName returns all packages matching the given platform and
// name, sorted by Version (string ascending). If no packages match, returns
// nil.
//
// This method never exposes map iteration order; results are always sorted.
func (idx *PackageIndex) LookupByPlatformName(
	platform types.PlatformId,
	name string,
) []types.Package {
	var matches []types.Package
	for _, pkg := range idx.pkgs {
		if pkg.Id.Platform == platform && pkg.Id.Name.String() == name {
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
