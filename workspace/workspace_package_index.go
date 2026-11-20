package workspace

import (
	"cmp"
	"sort"
)

// PackageIndex collects identities while keeping distinct installed paths.
// Adoption needs every conflicting file, not one representative by identity.
type PackageIndex struct {
	pkgs   map[string]DiscoveredPackage
	extras map[[2]string]DiscoveredPackage
}

func NewPackageIndex() *PackageIndex {
	return &PackageIndex{
		pkgs: make(map[string]DiscoveredPackage),
	}
}

// Add keeps the first observation of a package at a path. An observation with
// a path replaces a pathless one for the same identity.
func (idx *PackageIndex) Add(pkg DiscoveredPackage) {
	key := pkg.ID.StringQualified()

	if existing, exists := idx.pkgs[key]; exists {
		if pkg.Path == "" || existing.Path == pkg.Path {
			return
		}
		if existing.Path != "" {
			if idx.extras == nil {
				idx.extras = make(map[[2]string]DiscoveredPackage)
			}
			file := [2]string{key, pkg.Path}
			if _, ok := idx.extras[file]; !ok {
				idx.extras[file] = pkg
			}
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

// Packages returns the index in identity and path order.
func (idx *PackageIndex) Packages() []DiscoveredPackage {
	result := make([]DiscoveredPackage, 0, len(idx.pkgs)+len(idx.extras))
	for _, pkg := range idx.pkgs {
		result = append(result, pkg)
	}
	for _, pkg := range idx.extras {
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
		if c := cmp.Compare(pi.Version.String(), pj.Version.String()); c != 0 {
			return c < 0
		}
		if c := cmp.Compare(pi.Provider, pj.Provider); c != 0 {
			return c < 0
		}
		return result[i].Path < result[j].Path
	})

	return result
}
