package install

import (
	"fmt"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
)

// SnapshotInstalledConstraints reads installed packages from the probe snapshot
// and converts them into fixed InstalledConstraint entries.
// Each installed package is treated as an immutable anchor during recursive
// solving; it will never be auto-replaced by the solver.
func SnapshotInstalledConstraints() []InstalledConstraint {
	return snapshotInstalledConstraints(workspace.New())
}

func snapshotInstalledConstraints(si workspace.Workspace) []InstalledConstraint {
	constraints := make([]InstalledConstraint, 0, len(si.Packages)+3)
	seen := make(map[string]struct{}, len(si.Packages)+3)
	appendConstraint := func(pkg types.DiscoveredPackage, requester string) {
		if pkg.Id.Version.IsInvalid() {
			return
		}
		key := pkg.Id.StringBase()
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		constraints = append(
			constraints, InstalledConstraint{
				Package: pkg,
				ConstraintInput: resolve.ConstraintInput{
					Requester: requester,
					Dependency: types.Dependency{
						Id:        pkg.Id,
						Mandatory: true,
					},
				},
			},
		)
	}

	for _, pkg := range si.Packages {
		appendConstraint(pkg, fmt.Sprintf("installed:%s", pkg.Id.StringFull()))
	}

	if si.Server != nil {
		loader := si.DerivedModLoader()
		if loader.Valid() && loader != types.EcoUnspecified {
			gv := si.Server.GameVersion()
			if !gv.IsInvalid() && gv != types.VersionAny {
				appendConstraint(
					types.DiscoveredPackage{
						Id: types.VersionedPackageRef{
							PackageRef: types.PackageRef{
								Eco:  loader,
								Name: "minecraft",
							},
							Version: gv,
						},
					},
					fmt.Sprintf(
						"runtime:%s/minecraft@%s",
						loader,
						gv,
					),
				)
			}

			appendConstraint(
				types.DiscoveredPackage{
					Id: types.VersionedPackageRef{
						PackageRef: types.PackageRef{
							Eco:  loader,
							Name: "java",
						},
						Version: types.VersionAny,
					},
				}, fmt.Sprintf("runtime:%s/java", loader),
			)

			if primary := si.PrimaryRuntimeIdentity(); primary != nil {
				if alias := runtimeLoaderAliasName(primary.Eco); alias != "" {
					appendConstraint(
						types.DiscoveredPackage{
							Id: types.VersionedPackageRef{
								PackageRef: types.PackageRef{
									Eco:  loader,
									Name: alias,
								},
								Version: primary.Version,
							},
						},
						fmt.Sprintf(
							"runtime:%s/%s@%s",
							loader,
							alias,
							primary.Version,
						),
					)
				}
			}
		}
	}

	return constraints
}

func runtimeLoaderAliasName(platform types.Ecosystem) types.BarePackageName {
	switch platform {
	case types.EcoFabric:
		return "fabricloader"
	case types.EcoForge:
		return "forge"
	case types.EcoNeoforge:
		return "neoforge"
	default:
		return ""
	}
}

// FindCompatibleInstalled searches the installed-constraint snapshot for any
// package with the same platform and name as the requested ID, returning all
// matches. Results are informational only; the solver must not auto-select them.
func FindCompatibleInstalled(
	installedConstraints []InstalledConstraint,
	id types.VersionedPackageRef,
) []types.DiscoveredPackage {
	var matches []types.DiscoveredPackage
	for _, ic := range installedConstraints {
		pkg := ic.Package
		if pkg.Id.Eco != id.Eco {
			continue
		}
		if pkg.Id.Name != id.Name {
			continue
		}
		matches = append(matches, pkg)
	}
	return matches
}

// ReportCompatibleInstalled logs any locally-installed versions that are
// compatible with the given package ID. This is an informational-only report;
// no automatic selection occurs.
func ReportCompatibleInstalled(
	installedConstraints []InstalledConstraint,
	id types.VersionedPackageRef,
) {
	matches := FindCompatibleInstalled(installedConstraints, id)
	for _, pkg := range matches {
		log.ShowInfo(
			fmt.Sprintf(
				"[recursive] compatible installed version found: %s (not auto-selected)",
				pkg.Id.StringFull(),
			),
		)
	}
}
