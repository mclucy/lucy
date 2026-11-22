package bootstrap

import (
	"fmt"

	"github.com/mclucy/lucy/document"
)

// CheckExisting checks whether an existing server directory already satisfies the locked bootstrap contract
// without executing bootstrap.
// It verifies:
// - Exact presence and hashes of runtime locked inputs and runtime core artifacts. Stage-only inputs
// (Forge's bootstrap-only server.jar, server-mappings.txt, and .bootstrap-data/) are consumed by installer
// execution and absent from a committed tree, so they are neither required nor verified here.
// - Semantics and classpath of generated launch files.
// - Embedded metadata (download-context, versions.list, patches.list, libraries.list) for Paperclip.
// For methods where outputs are generated dynamically and cannot be proven identical to lock artifacts
// without execution (such as Forge installer processors), CheckExisting returns
// CurrentnessInconclusive and Current=false. Inconclusive must never be reported as success.
// When verification is inconclusive but not conflicting, verified attributable outputs are returned
// in Outputs so adoption can preserve them without claiming full bootstrap currentness.
// Definite conflicts and hash mismatches return CurrentnessMismatch.
func CheckExisting(serverDir string, locked document.LockedServer) (CheckResult, error) {
	return Check(serverDir, locked, "")
}

// Check verifies an existing or restored server tree against the locked
// bootstrap contract. coreFile, when supplied, is the materialized core
// artifact the lock pins: a Forge installer that declares processor outputs or
// a Paperclip jar that names its extracted libraries differently from the
// staged layout. It is read-only evidence and may be empty; without it a check
// proves only what the lock pins and the generated launchers declare.
func Check(serverDir string, locked document.LockedServer, coreFile string) (CheckResult, error) {
	exact, _, err := validateContract(locked)
	if err != nil {
		return CheckResult{
			Current: false,
			Status:  CurrentnessMismatch,
			Reason:  fmt.Sprintf("invalid bootstrap contract: %v", err),
		}, err
	}
	// An existing tree proves a root shim against the canonical copy under libraries.
	return newTreeVerifier(
		serverDir, locked, exact, false, nil,
		verifyLauncher(serverDir, locked), coreFile,
	).verify()
}
