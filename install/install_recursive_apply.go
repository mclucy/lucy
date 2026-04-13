package install

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
)

func recursiveInstallDestination(
	serverInfo types.ServerInfo,
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

	return serverInfo.WorkPath
}

// ApplyValidatedClosure executes the finalized install/remove plan after the
// recursive transaction has been committed.
func ApplyValidatedClosure(tx *RecursiveTransaction, serverInfo types.ServerInfo) error {
	if tx == nil {
		return errors.New("install: recursive transaction is nil")
	}
	if tx.Phase != PhaseCommitted {
		return fmt.Errorf("install: apply requires committed phase, got %d", tx.Phase)
	}
	if tx.Apply == nil {
		return errors.New("install: apply requires a validated apply plan")
	}

	if serverInfo.WorkPath != "" && serverInfo.WorkPath != "." {
		if err := os.MkdirAll(serverInfo.WorkPath, 0o755); err != nil {
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
					moveErrors = append(moveErrors, fmt.Errorf("create install directory for %s: %w", pkg.Id.StringFull(), err))
					continue
				}
			}
			dst := filepath.Join(dstDir, filepath.Base(src))
			if err := os.Rename(src, dst); err != nil {
				moveErrors = append(moveErrors, fmt.Errorf("move %s: %w", pkg.Id.StringFull(), err))
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
