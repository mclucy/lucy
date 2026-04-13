package install

import (
	"errors"
	"fmt"
	"os"

	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
)

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

	showRecursiveApplyStart(len(tx.Apply.Install))

	applied := 0
	applyErrors := make([]error, 0)

	for _, pkg := range tx.Apply.Install {
		installer := installers[pkg.Id.Platform]
		if installer == nil {
			installer = installers[types.PlatformAny]
		}
		if installer == nil {
			applyErrors = append(
				applyErrors,
				fmt.Errorf("%s: no installer found", pkg.Id.StringFull()),
			)
			continue
		}

		if err := installer(pkg); err != nil {
			applyErrors = append(
				applyErrors,
				fmt.Errorf("%s: %w", pkg.Id.StringFull(), err),
			)
			continue
		}

		applied++
	}

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
