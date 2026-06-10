package install

import (
	"fmt"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
)

// SnapshotInstalledConstraints reads installed packages from the probe snapshot
// and converts them into fixed InstalledConstraint entries for the transaction.
// Each installed package is treated as an immutable anchor during recursive
// solving; it will never be auto-replaced by the solver.
func SnapshotInstalledConstraints(tx *RecursiveTransaction) {
	si := probe.ServerInfo()
	constraints := make([]InstalledConstraint, 0, len(si.Packages)+3)
	seen := make(map[string]struct{}, len(si.Packages)+3)
	appendConstraint := func(pkg types.Package, requester string) {
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
				ConstraintInput: ConstraintInput{
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

	if si.Runtime != nil {
		loader := si.Runtime.DerivedModLoader()
		if loader.Valid() && loader != types.PlatformNone && loader != types.PlatformUnknown {
			if !si.Runtime.GameVersion.IsInvalid() && si.Runtime.GameVersion != types.VersionAny {
				appendConstraint(
					types.Package{
						Id: types.VersionedPackageRef{
							Platform: loader,
							Name:     types.BarePackageName("minecraft"),
							Version:  si.Runtime.GameVersion,
						},
					},
					fmt.Sprintf(
						"runtime:%s/minecraft@%s",
						loader,
						si.Runtime.GameVersion,
					),
				)
			}

			appendConstraint(
				types.Package{
					Id: types.VersionedPackageRef{
						Platform: loader,
						Name:     types.BarePackageName("java"),
						Version:  types.VersionAny,
					},
				}, fmt.Sprintf("runtime:%s/java", loader),
			)

			if primary := si.Runtime.PrimaryRuntimeIdentity(); primary != nil {
				if alias := runtimeLoaderAliasName(primary.IdentityToPlatform()); alias != "" {
					appendConstraint(
						types.Package{
							Id: types.VersionedPackageRef{
								Platform: loader,
								Name:     alias,
								Version:  primary.Version,
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

	tx.InstalledConstraints = constraints
}

func runtimeLoaderAliasName(platform types.PlatformId) types.BarePackageName {
	switch platform {
	case types.PlatformFabric:
		return types.BarePackageName("fabricloader")
	case types.PlatformForge:
		return types.BarePackageName("forge")
	case types.PlatformNeoforge:
		return types.BarePackageName("neoforge")
	default:
		return ""
	}
}

// FindCompatibleInstalled searches the installed-constraint snapshot for any
// package with the same platform and name as the requested ID, returning all
// matches. Results are informational only; the solver must not auto-select them.
func FindCompatibleInstalled(
	tx *RecursiveTransaction,
	id types.VersionedPackageRef,
) []types.Package {
	var matches []types.Package
	for _, ic := range tx.InstalledConstraints {
		pkg := ic.Package
		if pkg.Id.Platform != id.Platform {
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
	tx *RecursiveTransaction,
	id types.VersionedPackageRef,
) {
	matches := FindCompatibleInstalled(tx, id)
	for _, pkg := range matches {
		logger.ShowInfo(
			fmt.Sprintf(
				"[recursive] compatible installed version found: %s (not auto-selected)",
				pkg.Id.StringFull(),
			),
		)
	}
}
