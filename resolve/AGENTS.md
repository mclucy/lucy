# Package `resolve`

Resolves manifest and dependency requirements through candidate search, then validates the selected module graph against a target runtime.

## Responsibilities & Boundaries

- `Packages` runs candidate search, hard filtering, backtracking (`backtrack`), and module finalization (`finalizeModules`).
- Callers inject candidate listing (`CandidateSource`) and artifact materialization (`Materialize`). The package performs no network access and does not own routing, the network cache, or provider clients.
- `AnalysisCache` reuses verified descriptor analysis by artifact identity, filename, loader and Minecraft version. Incoming hash expectations must agree. Without an injected cache, reuse ends with the current search. The cache is safe for concurrent use and is seeded from `Previous` only when `PreviousTarget` has the same Minecraft version and descriptor loader as `Target`, because a descriptor reader selects its files by both: a pinned package whose recorded key and hashes match then needs no materialization.
- `CandidateCache` shares candidate listings across related searches in one command. A listing does not depend on a search's selectors, so it may be reused by every resolution of the same project, runtime, and ecosystem.

## Search

- **Overlap**: The search is sequential and deterministic. A `prefetcher` starts bounded listings for requests the search has learned and analyzes each listing's top-ranked candidate in the background. The search consumes those results in its own order; an error from unused speculation is dropped, and closing the search cancels remaining work. Artifact materialization runs under the context of the work that asked for it, so a transfer that only speculation started is abandoned when the search closes instead of being awaited.
- **Project Canonicalization**: A `<provider>:<slug>` request is rewritten to the provider's project ID, so a slug request and an ID request for one project share a selection key. The key is environment, ecosystem, provider, and project ID; at most one artifact is selected per key.
- **Provider Substitution**: A pure dependency demand (no direct requirement, no release hint) binds to a selected package when that package ranks before the demanded package in the runtime's descriptor precedence, answers every module identity the demanded package exposes, and satisfies the module constraints of the selected graph. Substitutions and pins from release hints are branch-local, so backtracking undoes them.
- **Demand Overrides Direct Bindings**: An active dependency demand overrides a disabled direct manifest requirement, forcing artifact selection and module validation.
- **Lock Replay**: `Previous` supplies the project identity and the locked file variant. `PreserveLocked` keeps that file selectable while the target is unchanged. A release may publish several files; only the primary answers an unconstrained requirement.
- **Search budget**: Only a new candidate branch after a failed selection counts toward `backtrackLimit`. Cancellation, an exhausted retry budget, or a manifest request with no eligible build stops the search. Dependency demands are branch-local. A duplicate native identity between direct roots can backtrack to the responsible choice without retrying unrelated selections.

## Validation

- **Module Identity**: Scoped by environment, module namespace, and normalized ID (`moduleKey`). Runtime modules (`runtimeModules`: Minecraft, loader, MCDR, `Target.CoreModules`) answer before package modules. Namespace and descriptor precedence come from the catalog (`Ecosystem.Loads`, `ModuleNamespace`); this package only applies them to the target.
- **Precedence**: A native module (not nested) outranks a bundled copy. Bundled copies in one package are alternative variants; the one the target satisfies wins. Copies from different packages compare versions. Two native modules with one identity in different packages are an error.
- **Conflicted Graphs**: `AcceptPackageConflicts` accepts package module problems and metadata conflicts declared in either direction during the first search, and reports them as warnings from the final graph; metadata-conflict warnings are deduplicated and deterministically ordered. Runtime-answered module problems, hard filters, materialization failures, and finalization errors remain fatal. An unreadable candidate descriptor rejects only that candidate. `AcceptPackageConflicts` is a first-search mode, not a retry.
- **Broken Graphs**: `AcceptBroken` retries with module checks relaxed only when the first search failed on candidate rejections; a stopped search is never retried. Otherwise an unsatisfied module graph is rejected.

## Targeted Tests

- Candidate decision & ordering: `go test ./resolve -run 'Test(DecideCandidate|OrderCandidatesNewestFirst)'`
- Metadata conflicts: `go test ./resolve -run TestMetadataConflict`
- Selection and backtracking fuzzers: `go test ./resolve -run 'FuzzPackages(ProjectSelection|BacktrackDemand|ModuleGraph)'`
