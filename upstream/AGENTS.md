# Upstream

Defines provider-neutral capability interfaces and normalized data conversion contracts for package discovery, resolution, and acquisition.

## Responsibilities & Invariants

- **Boundary**: `catalog` describes packages; `upstream` obtains remote metadata and artifacts. `upstream` never mutates workspace files or installs state.
- **Capability contracts**: Atomic interfaces (`Fetcher`, `DependencyResolver`, `Informer`, `Searcher`, `ArtifactMapper`, `VersionSelectorResolver`, `InstallerProvider`) define behavioral contracts. Concrete providers in `internal/providers/` implement these interfaces and depend on `upstream`, never the inverse.
- **Provider neutrality**: Callers consume capability interfaces; concrete provider clients remain package-private under `internal/providers/` and are bound via `upstream/routing`.
- **Local context**: `LocalContext` is an immutable snapshot of runtime facts (`MinecraftVersion`, `MCDRVersion`, `Ecosystem`) supplied by workspace discovery. `upstream` must not depend on discovery mechanics or filesystem state. Providers must never prompt for missing inputs; interactive input belongs to the command boundary.
- **Failure semantics**: Partial failures wrap as `ProviderError`. In `ArtifactLookup`, errors indicate an incomplete lookup rather than an artifact miss.

## Relevant Tests

- Provider conversion and response translation: `upstream/internal/providers/<provider>/*_test.go`.
