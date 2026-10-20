package version

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/types"
)

// Match rejects invalid expressions rather than treating parser failure as any.
func Match(raw, actual string, loader types.Ecosystem) (bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" {
		return true, nil
	}
	format := types.VersionSchemeSemver
	if loader == types.EcosystemForge || loader == types.EcosystemNeoForge {
		format = types.VersionSchemeMaven
	}
	v, err := Parse(types.Version(actual), format)
	if err != nil {
		return false, err
	}
	if v == nil {
		return false, fmt.Errorf("invalid version scheme %v value %q", format, actual)
	}
	for part := range strings.SplitSeq(raw, "||") {
		part = strings.TrimSpace(part)
		if loader == types.EcosystemFabric && strings.Contains(part, "-") {
			matched, handled, err := matchFabricMinimumPrerelease(part, actual)
			if err != nil {
				return false, err
			}
			if handled {
				if matched {
					return true, nil
				}
				continue
			}
		}
		expr := ParseRange(part, InferRangeDialect(loader), format)
		if expr == nil {
			return false, fmt.Errorf("invalid %s constraint %q", loader, part)
		}
		id := types.VersionedPackageRef{Name: "native", Ecosystem: loader}
		if (types.Dependency{ID: id, Constraint: expr}).Satisfy(id, v) {
			return true, nil
		}
	}
	return false, nil
}

// Fabric permits an empty prerelease as a boundary below all prereleases of
// the same numeric version. SemVer's empty prerelease denotes a release.
func matchFabricMinimumPrerelease(raw, actual string) (bool, bool, error) {
	tokens := strings.Fields(raw)
	handled := false
	for _, token := range tokens {
		handled = handled || strings.HasSuffix(token, "-")
	}
	if !handled {
		return false, false, nil
	}
	actualVersion := parseSemver(types.Version(strings.TrimSuffix(actual, "-")))
	if actualVersion == nil {
		return false, true, fmt.Errorf("invalid Fabric version %q", actual)
	}
	actualSemver := actualVersion.(*SemverVersion)
	actualBase := NewSemver(actualSemver.Major(), actualSemver.Minor(), actualSemver.Patch())
	for _, token := range tokens {
		if !strings.HasSuffix(token, "-") {
			expr := ParseRange(token, DialectFabricSemver, types.VersionSchemeSemver)
			if expr == nil {
				return false, true, fmt.Errorf("invalid Fabric criterion %q", token)
			}
			id := types.VersionedPackageRef{
				Name:      "native",
				Ecosystem: types.EcosystemFabric,
			}
			if !(types.Dependency{
				ID:         id,
				Constraint: expr,
			}).Satisfy(id, actualVersion) {
				return false, true, nil
			}
			continue
		}
		operator, number := "=", strings.TrimSuffix(token, "-")
		for _, op := range []string{">=", "<=", "==", "!=", ">", "<", "="} {
			if after, ok := strings.CutPrefix(number, op); ok {
				operator, number = op, after
				break
			}
		}
		boundary := parseSemver(types.Version(number))
		if boundary == nil {
			return false, true, fmt.Errorf("invalid Fabric boundary %q", token)
		}
		boundarySemver := boundary.(*SemverVersion)
		boundaryBase := NewSemver(boundarySemver.Major(), boundarySemver.Minor(), boundarySemver.Patch())
		comparison, isComparable := actualBase.Compare(boundaryBase)
		if !isComparable {
			return false, true, fmt.Errorf("incomparable Fabric boundary %q", token)
		}
		equal, less := comparison == 0, comparison < 0
		if equal && !strings.HasSuffix(actual, "-") {
			equal = false
		}
		greater := !less && !equal
		ok := false
		switch operator {
		case "=", "==":
			ok = equal
		case "!=":
			ok = !equal
		case ">":
			ok = greater
		case ">=":
			ok = greater || equal
		case "<":
			ok = less
		case "<=":
			ok = less || equal
		}
		if !ok {
			return false, true, nil
		}
	}
	return true, true, nil
}
