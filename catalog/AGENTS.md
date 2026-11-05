# catalog

Package `catalog` defines the domain model for package identities, artifacts, compatibility, metadata, and dependencies independently of their source or storage.

## Responsibilities

- **Identities**: Package names and references, provider identities, and package registries.
- **Artifacts & Modules**: Upstream release artifacts, embedded descriptor modules, and declared dependencies.
- **Dependencies & Metadata**: Dependency constraints, satisfaction origins, compatibility ecosystems, target runtime sides, and informational package metadata.

## Invariants & Safety

- **Strict Dependency Boundary**: Only imports the Go standard library and `github.com/mclucy/lucy/version`. Never import other internal packages or external libraries.
- **Domain-Only Types**: Pure data definitions and value representations. Performs no network requests, filesystem operations, workspace state management, or CLI presentation.
- **Placement Policy**: Shared usage alone across packages does not justify placing a type in `catalog`. Types belong here only when they represent source-agnostic package domain concepts.

## Relevant Tests

- Exercised through consumer suites across dependent packages:
  - Archive reading and descriptor parsing (`artifact`)
  - Manifest and lock document serialization (`document`)
  - Resolution and graph ordering (`resolve`, `internal/depgraph`)
