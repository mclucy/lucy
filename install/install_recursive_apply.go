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
	serverInfo workspace.Workspace,
	pkg types.Package,
) string {
	if pkg.Id.Platform.IsModding() && len(serverInfo.ModPath) > 0 {
		return serverInfo.ModPath[0]
	}

	if pkg.Id.Platform == types.PlatformMCDR &&
		serverInfo.Environments.Mcdr != nil &&
		len(serverInfo.Environments.Mcdr.Config.PluginDirectories) > 0 {
		return serverInfo.Environments.Mcdr.Config.PluginDirectories[0]
	}

	if len(serverInfo.ModPath) == 1 {
		return serverInfo.ModPath[0]
	}

	return serverInfo.Root
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
		if node.Package.Remote != nil {
			candidateByName[node.Package.Id.Name] = node
		}
	}

	keys := make([]string, 0, len(verified.VerifiedGraph))
	for key := range verified.VerifiedGraph {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	install := make([]types.Package, 0, len(keys))
	for _, key := range keys {
		verifiedPkg := verified.VerifiedGraph[key].Package

		candidate, ok := candidateGraph[key]
		if !ok || candidate.Package.Remote == nil {
			candidate, ok = candidateByName[verifiedPkg.Id.Name]
		}
		if !ok || candidate.Package.Remote == nil {
			return ApplyPlan{}, fmt.Errorf(
				"install: verified package %s is missing candidate remote metadata",
				verifiedPkg.Id.StringFull(),
			)
		}

		pkg := verifiedPkg
		pkg.Remote = candidate.Package.Remote
		install = append(install, pkg)
	}

	remove := make([]types.Package, 0)
	for _, extraId := range verified.ReconcileDiff.Extra {
		key := extraId.StringBase()
		node, ok := candidateGraph[key]
		if !ok {
			continue
		}
		if node.Package.Local == nil || node.Package.Local.Path == "" {
			continue
		}
		remove = append(remove, node.Package)
	}

	return ApplyPlan{Install: install, Remove: remove, Provenance: provenance}, nil
}

func applyPlan(
	ctx context.Context,
	plan ApplyPlan,
	serverInfo workspace.Workspace,
	journal Journal,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if serverInfo.Root != "" && serverInfo.Root != "." {
		if err := os.MkdirAll(serverInfo.Root, 0o755); err != nil {
			return fmt.Errorf("create server work path failed: %w", err)
		}
	}

	applied := 0

	recordEvent(journal, Event{Kind: EventApplyStart, Count: len(plan.Install)})

	if len(plan.Install) > 0 {
		var moveErrors []error
		for _, pkg := range plan.Install {
			if err := ctx.Err(); err != nil {
				moveErrors = append(moveErrors, err)
				break
			}
			if pkg.Local == nil || pkg.Local.Path == "" {
				continue
			}
			src := pkg.Local.Path
			dstDir := recursiveInstallDestination(serverInfo, pkg)
			if dstDir != "" && dstDir != "." {
				if err := os.MkdirAll(dstDir, 0o755); err != nil {
					moveErrors = append(
						moveErrors,
						fmt.Errorf(
							"create install directory for %s: %w",
							pkg.Id.StringFull(),
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
					fmt.Errorf("move %s: %w", pkg.Id.StringFull(), err),
				)
				continue
			}
			pkg.Local.Path = dst
			applied++
		}
		if len(moveErrors) > 0 {
			return errors.Join(moveErrors...)
		}
	}

	var applyErrors []error

	for _, pkg := range plan.Remove {
		if err := ctx.Err(); err != nil {
			applyErrors = append(applyErrors, err)
			break
		}
		if pkg.Local == nil || pkg.Local.Path == "" {
			continue
		}

		if err := os.Remove(pkg.Local.Path); err != nil {
			applyErrors = append(
				applyErrors,
				fmt.Errorf("remove %s: %w", pkg.Id.StringFull(), err),
			)
			continue
		}

		applied++
	}

	recordEvent(journal, Event{Kind: EventBatchSummary, Count: applied, Failed: len(applyErrors)})
	if len(applyErrors) > 0 {
		return errors.Join(applyErrors...)
	}

	workspace.InvalidateServerInfo()
	return nil
}
