# Package `resolve`

`resolve` selects package builds and validates module descriptor graphs against target server runtimes.

## Responsibilities & Boundaries

- Solves package dependency graphs through candidate search, hard filtering, and backtracking (`Packages`, `backtrack`).
- Validates and finalizes descriptor modules (`DescriptorModule`, `finalizeModules`) against runtime-provided modules (`runtimeModules`).
- **No network or routing ownership**: Callers inject candidate listing (`CandidateSource`) and artifact materialization callbacks (`Materialize`). `resolve` does not own or access the network cache, networks, or provider clients directly, though it provides optional session-scoped artifact analysis caching (`AnalysisCache`).

## Invariants & Behavior

- **Project Canonicalization**: Project slugs are canonicalized to upstream IDs during candidate search so each project appears at most once in the graph.
- **Module Identity & Precedence**: Module identity is scoped by environment, ecosystem, and normalized ID (`moduleKey`). Native loader modules outrank bundled copies.
- **Demand Overrides Direct Bindings**: An active dependency demand overrides a disabled direct manifest requirement, forcing artifact selection and module validation.
- **Search Limits & Hard Failures**: Total search attempts are bounded by `searchAttemptLimit` (`SearchLimitError`), and context cancellation aborts traversal. Operational failures (e.g. materialization, archive read, or candidate source errors) mark the search state so the final error surfaces the underlying operational cause directly rather than masking it behind candidate rejection summaries.
- **Conflicted Graphs**: `AcceptPackageConflicts` relaxes only package-caused problems during the first search: package module problems and metadata-declared incompatibilities, checked in both directions, are accepted. Module warnings come from the final selected graph, and metadata-conflict warnings are deterministically ordered, deduplicated pairs from that graph. Runtime-answered module problems, hard filters, and materialization or archive failures remain fatal, and operational errors are never downgraded to warnings. It is a first-search mode, not a retry.
- **Broken Graphs**: Retries with relaxed module checks only when `AcceptBroken` is set; otherwise rejects unsatisfied module graphs.

## Targeted Tests

- Candidate decision & ordering: `go test ./resolve -run 'Test(DecideCandidate|OrderCandidatesNewestFirst)'`
- Metadata conflicts: `go test ./resolve -run TestMetadataConflict`
- Selection and backtracking fuzzers: `go test ./resolve -run 'FuzzPackages(ProjectSelection|BacktrackDemand|ModuleGraph)'`
