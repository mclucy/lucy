package workspaceguard

import (
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
)

// RecoverInstall must run with workspace ownership before reading state for a
// new mutation. It is the only entry point that replays an interrupted
// installation. Journal detection stays read-only, so a workspace without a
// pending journal needs no runtime check; replay itself performs the mandatory
// inactivity check before any mutation.
func RecoverInstall(root string, logger *log.Logger) error {
	journal, err := workspacewrite.ReadInstallJournal(root)
	if err != nil {
		return err
	}
	if journal == nil {
		return nil
	}
	return workspacewrite.ReplayInstallJournal(root, journal, logger, CheckInactive)
}
