# Package discovery

`workspace/discovery` observes Minecraft server installations, evaluates member file ownership against receipts, and selects the active server runtime.

## Responsibilities & Invariants

- **Confined scan**: Scans server directories via a deterministic static recognizer registry; all filesystem and archive handles close before `Discover` returns.
- **Unified membership**: Discovered installation files belong to a single `Files` collection with typed roles; entry points are role filters, not separate collections.
- **Evidence and diagnostics**: Retains structural findings (conflicting identities, unreadable archives, alternative entry points) in `Diagnostics` without aborting discovery.
- **Receipt ownership**: `ObserveOwnership` verifies member files against `.lucy/install.yaml` using content hashes; paths alone never prove management, and unreadable files remain `OwnershipUnknown`.
- **Observation vs. receipt**: Distinguishes physical directory state from recorded management; discovery never writes or modifies receipt state.
- **Deterministic selection**: `Select` applies explicit entry point first, declared identity second, and inferred preference third, returning structured `SelectionError` values on failure.
- **Read-only evaluation**: Scans, ownership checks, and runtime selections never modify files on disk or make network calls.

## Testing

- Unit tests: `go test -count=1 ./workspace/discovery`
- E2E discovery validation: `go test -tags e2e -count=1 ./e2e/status`
