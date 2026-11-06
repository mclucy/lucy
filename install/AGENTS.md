# install

Convergence engine for workspace runtime files, documents, and receipts.

## Responsibilities & Boundaries

- Executes convergence (`Sync`, `PlanInstall`, `ApplyPlan`): resolves or replays locks, stages runtime trees, and atomically commits changes.
- Staging vs workspace ownership: materializes locked server inputs and enabled packages into a private staging directory. Staged files never become owned workspace state until committed.
- Manifest, lock, and receipt semantics: manifest (`lucy.yaml`) defines intent, lock (`lucy-lock.yaml`) pins exact resolutions, and receipt (`.lucy/install.yaml`) records committed outputs, conflict baseline hashes, modes, and origins (`bootstrap`, `package`, or `mcdr`). Receipt validity separates `receipt.BootstrapFingerprint` (server bootstrap resolution and contract satisfaction) from `receipt.PackageFingerprint` (packages and MCDR wrapper). Java requirements, tracked files, and manifest fingerprints do not invalidate it.
- Skipping staging: if receipt satisfies the bootstrap contract, server bootstrap staging is bypassed; package-only operations retain accepted bootstrap outputs. Manifest publication on a current runtime commits documents without restaging.

## Invariants & Transaction Safety

- Path validation: verifies workspace confinement (`SafePath`), rejects path names that alias the same target filesystem entry, and reserves `.lucy/commit.yaml`, `lucy.yaml`, and `lucy-lock.yaml`.
- Verification, commit, and rollback:
  - Preserves before-images of replaced or pruned files into a temporary workspace-relative `.lucy/rollback-*` directory before publishing `.lucy/commit.yaml`.
  - Accepted entries already absent at planning stay preconditions through journal planning and the pre-commit check; a path that reappears fails the install and is never deleted.
  - Atomically writes files, lock, manifest (if publishing), and receipt; then marks the journal `Committed = true`.
  - Point of no return: once marked committed, recovery is cleanup-only. Failures during rollback/journal cleanup or post-commit tracked-file provisioning return `CommittedError`; committed files and documents are never rolled back.
- Recovery and inactivity protection:
  - Interrupted transactions are recovered under `.lucy/writer.lock` via `RecoverLocked`/`RecoverInstall`.
  - Journal detection is passive; journal replay requires an inactivity check verifying server processes are stopped before any disk mutation.
  - Replay checks pre-images and aborts rather than overwriting intervening user edits. Stale staging debris is swept on acquiring the writer lock.

## Relevant Tests

- `go test -count=1 ./install`
- `go test -tags e2e -count=1 ./e2e/install -run 'TestInstall/<scenario id>'`
- `go test -tags e2e -count=1 ./e2e/install -run 'TestInitInstall/<sandbox id>'`
