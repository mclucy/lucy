# receipt

Package `receipt` owns the workspace installation receipt (`.lucy/install.yaml`): the authoritative ledger of committed runtime outputs and the resolution it answers.

## Responsibilities & Ownership

- **Committed State Ledger**: Records workspace-relative paths, SHA-256 content hashes, and file modes for outputs committed by installation or package adoption.
- **Authoritative Ownership**: Lucy owns only files recorded in the receipt. Files produced during staging become owned workspace state only upon commit; unrecorded files are treated as unmanaged.
- **Atomic Persistence**: Provides atomic file replacement (`Write`) and memory snapshots (`Snapshot`) for transactional commit workflows.

## Fingerprint Relationship

- **Resolution Fingerprint**: The receipt records `LockFingerprint`, computed via `ResolutionFingerprint(lock)`.
- **Targeted Lock Digest**: `ResolutionFingerprint` marshals only the server, MCDR, and package pins of `document.Lock` as YAML and returns an unprefixed hex digest.
- **Invariance**: Explicitly excludes tracked files, the manifest fingerprint, and Java requirements. Altering those lock fields does not alter installed files and does not invalidate the receipt.

## Consumers

- `install`: Checks if the installed state is current against `ResolutionFingerprint`, manages transactions, and commits updated receipts.
- `workspace/discovery`: Reads the receipt to distinguish managed files from unmanaged workspace entries.
- `filetrack`: Uses receipt entries to verify file integrity and detect modified files.

## Relevant Tests

- `operation/operation_filetrack_workspace_test.go`: Resolution fingerprinting and tracked file behavior.
- `install/install_pipeline_test.go`: Staged installation commits and receipt updates.
- `e2e/install/install_init_test.go`: Installation and adoption receipt verification.
