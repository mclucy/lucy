package workspace

import (
	"cmp"
	"sort"

	"github.com/mclucy/lucy/catalog"
)

// PackageIndex is a map-backed index over discovered packages, keyed by
// the qualified identity StringQualified(). All exported methods return
// results in a stable order; raw map iteration order never reaches callers.
type PackageIndex struct {
	pkgs map[string]DiscoveredPackage
}

func NewPackageIndex() *PackageIndex {
	return &PackageIndex{
		pkgs: make(map[string]DiscoveredPackage),
	}
}

// Add inserts a package into the index. First-write wins, except that a
// non-empty Path replaces an existing entry whose Path is empty, so an
// entry without a local path can gain one when discovered.
func (idx *PackageIndex) Add(pkg DiscoveredPackage) {
	key := pkg.ID.StringQualified()

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
func (idx *PackageIndex) Merge(pkgs []DiscoveredPackage) {
	for _, pkg := range pkgs {
		idx.Add(pkg)
	}
}

// Packages returns all indexed packages sorted ascending by platform,
// name, and version string.
func (idx *PackageIndex) Packages() []DiscoveredPackage {
	result := make([]DiscoveredPackage, 0, len(idx.pkgs))
	for _, pkg := range idx.pkgs {
		result = append(result, pkg)
	}

	sort.Slice(result, func(i, j int) bool {
		pi, pj := result[i].ID, result[j].ID
		if c := cmp.Compare(pi.Ecosystem.String(), pj.Ecosystem.String()); c != 0 {
			return c < 0
		}
		if c := cmp.Compare(pi.Name.String(), pj.Name.String()); c != 0 {
			return c < 0
		}
		return cmp.Compare(pi.Version.String(), pj.Version.String()) < 0
	})

	return result
}

// LookupByID performs an exact lookup by the qualified identity
// (StringQualified()), returning a zero package and false when absent.
func (idx *PackageIndex) LookupByID(id catalog.VersionedRef) (
	DiscoveredPackage,
	bool,
) {
	pkg, ok := idx.pkgs[id.StringQualified()]
	return pkg, ok
}

// LookupByEcosystemName returns all packages matching the given platform
// and name, sorted by version string ascending, or nil when none match.
func (idx *PackageIndex) LookupByEcosystemName(
	platform catalog.Ecosystem,
	name string,
) []DiscoveredPackage {
	var matches []DiscoveredPackage
	for _, pkg := range idx.pkgs {
		if pkg.ID.Ecosystem == platform && pkg.ID.Name.String() == name {
			matches = append(matches, pkg)
		}
	}

	if len(matches) == 0 {
		return nil
	}

	sort.Slice(matches, func(i, j int) bool {
		return cmp.Compare(matches[i].ID.Version.String(), matches[j].ID.Version.String()) < 0
	})

	return matches
}
