package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
)

func recursiveInstallDestination(
	ws workspace.Workspace,
	pkg types.InstalledPackage,
) string {
	if pkg.Id.Eco.IsModding() && len(ws.ModPath()) > 0 {
		return ws.ModPath()[0]
	}

	if pkg.Id.Eco == types.EcoMcdr &&
		ws.Environments.Mcdr != nil &&
		len(ws.Environments.Mcdr.Config.PluginDirectories) > 0 {
		return ws.Environments.Mcdr.Config.PluginDirectories[0]
	}

	if len(ws.ModPath()) == 1 {
		return ws.ModPath()[0]
	}

	return ws.Root
}

func planApply(
	ctx context.Context,
	verified VerifiedClosure,
	_ []InstalledConstraint,
) (ApplyPlan, error) {
	if err := ctx.Err(); err != nil {
		return ApplyPlan{}, err
	}

	candidateGraph := verified.Downloaded.Resolved.CandidateGraph
	provenance := make(map[string][]string, len(candidateGraph))
	for key, node := range candidateGraph {
		provenance[key] = append([]string(nil), node.ProvenancePath...)
	}

	candidateByName := make(
		map[types.BarePackageName]CandidateNode,
		len(candidateGraph),
	)
	for _, node := range candidateGraph {
		if node.Package.FileUrl != "" {
			candidateByName[node.Package.Id.Name] = node
		}
	}

	keys := make([]string, 0, len(verified.VerifiedGraph))
	for key := range verified.VerifiedGraph {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	install := make([]types.InstalledPackage, 0, len(keys))
	for _, key := range keys {
		verifiedNode := verified.VerifiedGraph[key]
		verifiedPkg := verifiedNode.Package

		candidate, ok := candidateGraph[key]
		if !ok || candidate.Package.FileUrl == "" {
			candidate, ok = candidateByName[verifiedPkg.Id.Name]
		}
		if !ok || candidate.Package.FileUrl == "" {
			return ApplyPlan{}, fmt.Errorf(
				"install: verified package %s is missing candidate remote metadata",
				resolvedPackageLabel(verifiedPkg),
			)
		}

		resolved := candidate.Package
		resolved.Id = types.FullPackageRef{
			PackageRef: verifiedPkg.Id.PackageRef,
			Version:    verifiedPkg.Id.Version,
			Scope:      candidate.Package.Id.Scope,
		}

		pkg := types.InstalledPackage{
			ResolvedPackage: resolved,
			Path:            verifiedNode.Path,
		}
		if verifiedNode.Dependencies != nil {
			pkg.Dependencies = *verifiedNode.Dependencies
		}
		install = append(install, pkg)
	}

	remove := make([]types.InstalledPackage, 0)
	for _, extraId := range verified.ReconcileDiff.Extra {
		key := extraId.StringBase()
		node, ok := candidateGraph[key]
		if !ok {
			continue
		}
		if node.Path == "" {
			continue
		}
		remove = append(
			remove, types.InstalledPackage{
				ResolvedPackage: node.Package,
				Path:            node.Path,
			},
		)
	}

	return ApplyPlan{
		Install: install, Remove: remove, Provenance: provenance,
	}, nil
}

func applyPlan(
	ctx context.Context,
	plan ApplyPlan,
	ws workspace.Workspace,
	journal Journal,
) (ApplyPlan, error) {
	if err := ctx.Err(); err != nil {
		return plan, err
	}

	if ws.Root != "" && ws.Root != "." {
		if err := os.MkdirAll(ws.Root, 0o755); err != nil {
			return plan, fmt.Errorf("create server work path failed: %w", err)
		}
	}

	applied := 0

	recordEvent(journal, Event{Kind: EventApplyStart, Count: len(plan.Install)})

	if len(plan.Install) > 0 {
		var moveErrors []error
		for i, pkg := range plan.Install {
			if err := ctx.Err(); err != nil {
				moveErrors = append(moveErrors, err)
				break
			}
			if pkg.Path == "" {
				continue
			}
			src := pkg.Path
			dstDir := recursiveInstallDestination(ws, pkg)
			if dstDir != "" && dstDir != "." {
				if err := os.MkdirAll(dstDir, 0o755); err != nil {
					moveErrors = append(
						moveErrors,
						fmt.Errorf(
							"create install directory for %s: %w",
							resolvedPackageLabel(pkg.ResolvedPackage),
							err,
						),
					)
					continue
				}
			}
			dst := filepath.Join(dstDir, filepath.Base(src))
			if err := os.Rename(src, dst); err != nil {
				moveErrors = append(
					moveErrors,
					fmt.Errorf(
						"move %s: %w",
						resolvedPackageLabel(pkg.ResolvedPackage),
						err,
					),
				)
				continue
			}
			plan.Install[i].Path = dst
			applied++
		}
		if len(moveErrors) > 0 {
			return plan, errors.Join(moveErrors...)
		}
	}

	var applyErrors []error

	for _, pkg := range plan.Remove {
		if err := ctx.Err(); err != nil {
			applyErrors = append(applyErrors, err)
			break
		}
		if pkg.Path == "" {
			continue
		}

		if err := os.Remove(pkg.Path); err != nil {
			applyErrors = append(
				applyErrors,
				fmt.Errorf(
					"remove %s: %w",
					resolvedPackageLabel(pkg.ResolvedPackage),
					err,
				),
			)
			continue
		}

		applied++
	}

	recordEvent(
		journal,
		Event{
			Kind: EventBatchSummary, Count: applied, Failed: len(applyErrors),
		},
	)
	if len(applyErrors) > 0 {
		return plan, errors.Join(applyErrors...)
	}

	workspace.Invalidate()
	return plan, nil
}
