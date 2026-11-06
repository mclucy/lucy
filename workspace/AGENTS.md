# Package workspace

`workspace` coordinates local-only observation of a Minecraft server directory. It never makes network calls and never modifies workspace state.

## Responsibilities & Invariants

- **State separation**: Distinguishes live filesystem observation from persisted intent (`document` manifest/lock) and installation records (`.lucy/install.yaml`). Observation describes what currently exists on disk.
- **Assembly**: Combines installation discovery (`workspace/discovery`) with wrapper detection (MCDR working and plugin paths) and local package inventories.
- **Ordered selection**: Resolves runtime selection before scanning package directories, ensuring mod and plugin search paths match the selected runtime ecosystem.
- **Reselection**: `Select` switches the active runtime and refreshes package observations for new directories without rescanning installations on disk.
- **Deferred identification**: Discovers local packages from archive descriptors only; unidentified JARs are collected for explicit hash lookup via `workspace/identify`.
- **Runtime activity**: Checks server process execution via session lock files on save directories; performs fresh I/O on every query.

## Testing

- Unit tests: `go test -count=1 ./workspace`
- E2E status probes: `go test -tags e2e -count=1 ./e2e/status`
