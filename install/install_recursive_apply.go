package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
)

func recursiveInstallDestination(
	serverInfo probe.Workspace,
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

func BuildRecursiveApplyPlan(tx *RecursiveTransaction) (ApplyPlan, error) {
	if tx == nil {
		return ApplyPlan{}, fmt.Errorf("install: nil recursive transaction")
	}

	candidateByName := make(
		map[types.PackageName]CandidateNode,
		len(tx.CandidateGraph),
	)
	for _, node := range tx.CandidateGraph {
		if node.Package.Remote != nil {
			candidateByName[node.Package.Id.Name] = node
		}
	}

	keys := make([]string, 0, len(tx.VerifiedGraph))
	for key := range tx.VerifiedGraph {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	install := make([]types.Package, 0, len(keys))
	for _, key := range keys {
		verified := tx.VerifiedGraph[key].Package

		candidate, ok := tx.CandidateGraph[key]
		if !ok || candidate.Package.Remote == nil {
			candidate, ok = candidateByName[verified.Id.Name]
		}
		if !ok || candidate.Package.Remote == nil {
			return ApplyPlan{}, fmt.Errorf(
				"install: verified package %s is missing candidate remote metadata",
				verified.Id.StringFull(),
			)
		}

		pkg := verified
		pkg.Remote = candidate.Package.Remote
		install = append(install, pkg)
	}

	remove := make([]types.Package, 0)
	for _, extraId := range tx.ReconcileDiff.Extra {
		key := extraId.StringPlatformName()
		node, ok := tx.CandidateGraph[key]
		if !ok {
			continue
		}
		if node.Package.Local == nil || node.Package.Local.Path == "" {
			continue
		}
		remove = append(remove, node.Package)
	}

	return ApplyPlan{Install: install, Remove: remove}, nil
}

// ApplyValidatedClosure executes the finalized install/remove plan after the
// recursive transaction has been committed.
func ApplyValidatedClosure(
	tx *RecursiveTransaction,
	serverInfo probe.Workspace,
) error {
	if tx == nil {
		return errors.New("install: recursive transaction is nil")
	}
	if tx.Phase != PhaseCommitted {
		return fmt.Errorf(
			"install: apply requires committed phase, got %d",
			tx.Phase,
		)
	}
	if tx.Apply == nil {
		return errors.New("install: apply requires a validated apply plan")
	}

	if serverInfo.Root != "" && serverInfo.Root != "." {
		if err := os.MkdirAll(serverInfo.Root, 0o755); err != nil {
			return fmt.Errorf("create server work path failed: %w", err)
		}
	}

	applied := 0

	showRecursiveApplyStart(len(tx.Apply.Install))

	if tx.StagingDir != "" && len(tx.Apply.Install) > 0 {
		var moveErrors []error
		for _, pkg := range tx.Apply.Install {
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

	for _, pkg := range tx.Apply.Remove {
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

	showBatchSummary(applied, len(applyErrors))
	if len(applyErrors) > 0 {
		return errors.Join(applyErrors...)
	}

	probe.InvalidateServerInfo()
	return nil
}
