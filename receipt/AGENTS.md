# receipt

Package `receipt` owns the workspace installation receipt (`.lucy/install.yaml`): the authoritative ledger of committed runtime outputs and the resolutions they answer.

## Responsibilities & Ownership

- **Committed State Ledger**: Records workspace-relative paths, SHA-256 conflict baseline digests, file modes, and origins (`bootstrap`, `package` with package identity, or `mcdr` for the independently reconciled wrapper runtime) for managed outputs committed by installation or adoption.
- **Authoritative Ownership**: Lucy owns only files recorded in the receipt. Seed files are initialized when missing but user-owned thereafter, discard files are staging-only, and unrecorded files remain unmanaged.
- **Persistence**: Provides canonical memory snapshots (`Snapshot`); installation publishes those bytes through the workspace transaction journal.
- **Strict Validation**: Rejects unnormalized or unsafe paths, reserved filenames, path collisions (case-insensitive), and invalid digests. Legacy un-attributed receipts return `ErrLegacyReceipt` giving explicit remediation (`lucy init` or `lucy init --force`).

## Fingerprint Relationship

- **Bootstrap Fingerprint**: `BootstrapFingerprint(lock)` digests the server resolution fields (product, method, core artifact, inputs, directory), strictly excluding Java requirements, tracked files, MCDR, and packages.
- **Package Fingerprint**: `PackageFingerprint(lock)` digests the package pins and MCDR wrapper directory.
- **Invariance**: Explicitly excludes tracked files, the manifest fingerprint, and Java requirements. Ordinary package changes do not invalidate an unchanged, satisfied bootstrap.

## Consumers

- `install`: Checks if the installed state is current against `BootstrapFingerprint` and `PackageFingerprint`, manages transactions, and commits updated receipts.
- `workspace/discovery`: Reads the receipt to distinguish managed files from unmanaged workspace entries.
- `filetrack`: Uses receipt entries to verify file integrity and detect modified files.

## Relevant Tests

- `operation/operation_filetrack_workspace_test.go`: Resolution fingerprinting and tracked file behavior.
- `install/install_pipeline_test.go`: Staged installation commits and receipt updates.
