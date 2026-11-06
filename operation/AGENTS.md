# Package `operation`

`operation` orchestrates workspace lifecycle commands (`Create`, `Initialize`, `Sync`, `Add`, `Remove`, `Upgrade`, `Deinitialize`, `RenameServer`, `SetJava`).

## Responsibilities & Boundaries

- Coordinates multi-step workspace workflows across `install.Service`, routing `Source`, `serverregistry.Registry`, and document models.
- Prepares documents, evaluates server runtime environments, and coordinates upgrade Pareto frontiers without owning low-level file transactions.

## Invariants & Behavior

- **Explicit Dependency Injection**: `Service` never accesses global singletons. Dependencies (`install.Service`, `Source`, `serverregistry.Registry`, `log.Logger`) must be passed explicitly into `NewService`.
- **Workspace Locking & Recovery**: Mutating operations serialize through the workspace writer lock (`workspacewrite.Lock` on `.lucy/writer.lock`) and run journal recovery (e.g. `filetrack.RecoverLocked` or `workspaceguard.RecoverInstall`) before applying mutations. Read-heavy workflows (e.g. `PlanUpgrade` preflight or target inspection) may inspect documents or paths prior to acquiring the lock.
- **Retention Leases & Registry**: Candidate-resolving workflows (`Create`, `Initialize`, `PlanUpgrade`) acquire an artifact store lease (`artifactstore.AcquireOperation`) to shield staged files from retention sweeps. Workspaces are recorded in `serverregistry.Registry` only after their lock is visible; initialization registers after its manifest/lock/receipt transaction commits and before the operation lease closes, because the registry reads the manifest and the sweep must never trace a workspace whose documents were never published.
- **Document Pinning**: Workflows capture manifest and lock fingerprints (`pinWorkspaceDocuments`) before passing options to `install.Service`, rejecting concurrent external workspace edits.
- **Phased Error Recovery**: Distinct phase errors (`CreateResolveError`, `CreatePublishError`, `CreateRegistrationError`, `CreateInstallError`) distinguish pre-publish aborts from post-publish installation failures where declared files remain intact for retry.

## Targeted Tests

- Workspace initialization & selection: `go test ./operation -run TestInitializeWritesDeclarationForVanillaLayout`
- File tracking and runtime pins: `go test ./operation -run TestFileCommandsPreserveRuntimePinsAndReceipt`
- Server rename safety: `go test ./operation -run TestRenameServerChangesOnlyManifestName`
- Upgrade planning & Pareto selection: `go test ./operation -run 'Test(ParetoFrontierDropsDominatedOptions|SelectUpgradeOption|UpgradePlanHashIsOrderIndependent)'`
