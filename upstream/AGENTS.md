# Upstream

Defines provider-neutral capability interfaces and normalized data conversion contracts for package discovery, resolution, and acquisition.

## Responsibilities & Invariants

- **Boundary**: `catalog` describes packages; `upstream` obtains remote metadata and artifacts. `upstream` never mutates workspace files or installs state.
- **Capability contracts**: `PackageCatalogue` supplies repository builds and candidates. `Searcher`, `Informer`, `ArtifactMapper`, and their provider-identified compositions define the other routing contracts. Concrete server-product operations remain on the Mojang and loader-meta providers.
- **Provider neutrality**: Callers consume capability interfaces; concrete provider clients remain package-private under `internal/providers/` and are bound via `upstream/routing`.
- **Runtime inputs**: Server-product metadata operations receive the requested Minecraft version explicitly. Upstream must not depend on workspace discovery mechanics or filesystem state. Providers must never prompt for missing inputs; interactive input belongs to the command boundary.
- **Failure semantics**: Partial failures wrap as `ProviderError`. An unmatched `ArtifactReleaseResolution` with errors is an incomplete lookup, not an artifact miss. A match can retain earlier mapper errors.

## Relevant Tests

- Provider conversion and response translation: `upstream/internal/providers/<provider>/*_test.go`.
