# install

Convergence engine for workspace runtime files, documents, and receipts.

## Responsibilities & Boundaries

- Executes convergence (`Resolve`, `PlanFromResolution`, `Sync`, `ApplyPlan`): resolves or replays locks, stages runtime trees, and atomically commits changes.
- Staging vs workspace ownership: materializes locked server inputs and enabled packages into a private staging directory. Staged files never become owned workspace state until committed.
- Runtime ownership: the files a bootstrap method produces (server jars, libraries, versions, cache, launchers, run scripts) are owned as the runtime image of the lock's recipe. `--overwrite` governs package destinations only.
- Manifest, lock, and receipt semantics: manifest (`lucy.yaml`) defines intent, lock (`lucy-lock.yaml`) pins exact resolutions, and receipt (`.lucy/install.yaml`) records committed outputs, baseline hashes, modes, and origins (`bootstrap`, `package`, or `mcdr`). Receipt validity separates `receipt.BootstrapFingerprint` (runtime currentness: lock recipe key plus owned runtime digests and recorded seed presence) from `receipt.PackageFingerprint` (packages and MCDR wrapper). A recorded seed path must be a present regular file; its content stays user-owned. Java requirements, tracked files, and manifest fingerprints do not invalidate it.
- Skipping staging: if receipt satisfies the bootstrap contract, server bootstrap staging is bypassed; package-only operations retain accepted bootstrap outputs. Manifest publication on a current runtime commits documents without restaging.

## Invariants & Transaction Safety

- Path validation: verifies workspace confinement (`SafePath`), rejects path names that alias the same target filesystem entry, and reserves `.lucy/commit.yaml`, `lucy.yaml`, and `lucy-lock.yaml`.
- Verification, commit, and rollback:
  - Preserves before-images of replaced or pruned files into a temporary workspace-relative `.lucy/rollback-*` directory before publishing `.lucy/commit.yaml`.
  - Displacement: a replaced runtime output that Lucy does not own, or owns with a changed digest, keeps its before-image under `.lucy/displaced/<transaction-id>/` with an `index.yaml` (path, sha256, mode). The journal carries the flag; execution happens only after the commit point, recovery before it restores the original files, and Lucy never deletes displaced content.
  - Accepted entries already absent at planning stay preconditions through journal planning and the pre-commit check; a path that reappears fails the install and is never deleted.
  - Atomically writes files, lock, manifest (if publishing), and receipt; then marks the journal `Committed = true`.
  - Point of no return: once marked committed, recovery is cleanup-only. Failures during rollback/journal cleanup or post-commit tracked-file provisioning return `CommittedError`; committed files and documents are never rolled back.
- Recovery and inactivity protection:
  - Interrupted transactions are recovered under `.lucy/writer.lock` via `RecoverLocked`/`RecoverInstall`.
  - Journal detection is passive; journal replay requires an inactivity check verifying server processes are stopped before any disk mutation.
  - Replay checks pre-images and aborts rather than overwriting intervening user edits. Stale staging debris is swept on acquiring the writer lock.

## Relevant Tests

- `go test -count=1 ./install`
