# Upstream Routing

Applies provider selection policy, parallel execution strategies, and server product resolution across configured upstream providers.

## Responsibilities & Policy

- **Provider composition**: `Router` instantiates and wires private provider implementations over a shared network cache and optional credentials (such as CurseForge API key). Callers interact through capability interfaces.
- **Candidate selection**: `ResolveSearchProviders` and `ResolveInfoProviders` map an ecosystem to ordered provider capabilities. Unspecified platforms use the default auto list (Modrinth, optional CurseForge, MCDR). Static capability tables define ecosystem support. Repository builds and candidates dispatch through typed `PackageCatalogue` entries rather than per-provider switches.

## Execution & Provenance

- **Parallel fan-out**: `SearchMany` queries providers concurrently, returning all successful result sets while accumulating non-fatal `ProviderError` entries.
- **First-success racing**: `GetInfoFirstSuccess` races providers in parallel, returning the first successful response tagged with winning source provenance on `catalog.Ref.Provider`.
- **Ordered hash lookup**: `ResolveArtifactByHash` checks hash mappers sequentially by priority; errors yield an incomplete lookup rather than halting subsequent candidates.
- **Server product resolution**: `ResolveServerProduct` resolves vanilla and loader server runtimes (Mojang, Fabric, Quilt, Paperclip, GitHub forks), enforcing trusted Maven sidecar checksums or API digest manifests before staging.

## Architecture Invariants

- **IoC boundary**: Retain operation-specific capability interfaces at the execution boundary. The internal provider catalog is compiled plumbing, not a dynamic dependency-injection container.
- **Policy authority**: Maintain selection and ecosystem rules as centralized authoritative data tables rather than ad-hoc switch statements in callers.

