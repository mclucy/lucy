# install/

The installation pipeline for Lucy. This package takes package requests, resolves their dependencies, downloads artifacts, verifies them against actual jar metadata, and installs them into a Minecraft server directory.

## The Metadata Verification Problem

The central design challenge of the install pipeline is that **upstream dependency metadata cannot be trusted**. Modrinth, CurseForge, and other registries provide dependency lists via their APIs, but these lists are frequently incomplete, incorrect, or stale — mod authors may not maintain them, and the registry has no way to enforce correctness.

The only source of truth for what a package actually depends on is the artifact itself: `fabric.mod.json`, `mods.toml`, `plugin.yml`, and similar loader-specific metadata files embedded in the jar. But you cannot read jar metadata without downloading the jar first.

This creates a fundamental ordering problem:

1. **Resolve** dependencies using upstream API metadata (which may be wrong)
2. **Download** the resolved artifacts
3. **Verify** the actual dependencies from jar metadata
4. **Discover** that the real dependency graph differs from what the API said
5. Go back to step 1 with corrected information

This is why the install pipeline has a **reconcile loop** — it iterates until the resolved graph stabilizes against verified artifact metadata, or gives up after a bounded number of attempts.

### Advisory vs Verified Nodes

The pipeline tracks metadata provenance through two graph layers:

- **Advisory nodes** (candidate graph): Built from upstream API metadata. These may be wrong. They represent "what the registry claims."
- **Verified nodes** (verified graph): Built from actual artifact metadata after download. These are ground truth. They represent "what the jar actually declares."

The reconcile stage compares these two graphs. When they differ — missing dependencies, extra dependencies, tightened version constraints — the pipeline re-resolves with corrected information and iterates.

`PackageDependencies.Authentic` tracks this distinction: `false` means from the upstream API, `true` means from the artifact.

### Constraint Solving

The constraint solver (`MergeConstraintGraph`) is a pure function with zero I/O. It merges version constraint inputs from all packages into a unified constraint graph, detecting conflicts (unsatisfiable version requirements) and producing a merged requirement set.

The solver operates on DNF (disjunctive normal form) version expressions: each package's version constraint is a 2D array (outer OR, inner AND) of version sub-expressions. Merging two constraints takes their Cartesian product of variants, checks each conjunction for satisfiability, and prunes dead branches.

### Platform Normalization

The Minecraft ecosystem has ambiguous platform identifiers. A package may declare `platform: any` or `platform: none` meaning different things in different contexts. The reconcile stage normalizes these during graph comparison — a candidate with `platform: none` can match a verified node with a specific platform, as long as the package name matches.

## Package Lifecycle

A package goes through these stages during installation:

1. **Requested** — user input parsed into a `PackageRequest` (source + platform + name + version)
2. **Resolved** — upstream provider returns version metadata and download URL
3. **Downloaded** — artifact fetched to a staging directory via the network cache
4. **Verified** — jar metadata read, dependencies extracted, checksums confirmed
5. **Committed** — artifact moved from staging to its final location in the server directory

Each stage adds information. Resolution adds remote metadata (URL, hash, filename). Download adds the local staging path. Verification replaces API-sourced dependencies with artifact-sourced dependencies. Commitment sets the final install path.

## Identity Packages vs Regular Packages

The `Install` entry point distinguishes between two fundamentally different operations:

- **Identity packages** (Minecraft, Fabric, Forge, NeoForge, MCDReforged): These bootstrap the server platform. Installing Forge means running a Java installer subprocess, rewriting the server binary, accepting the EULA. These are irreversible, platform-altering operations with different failure semantics than regular packages.

- **Regular packages** (mods, plugins, datapacks): These are jar files placed in the appropriate directory. Installation is an atomic `os.Rename` from staging to destination. Removal is `os.Remove`. These go through the full recursive dependency resolution pipeline.

The two paths share no operational logic. They are separate concerns that happen to share an entry point.

## Gotchas

- **The reconcile loop can fail.** If advisory and verified metadata never converge, the loop hits a bounded iteration limit. This is not a bug — it means the upstream metadata is so wrong that resolution cannot stabilize. The error should guide the user to report the upstream metadata issue.
- **Downloads happen inside the reconcile loop.** The current design downloads artifacts before knowing whether the graph is stable. This means some downloads may be wasted if the graph changes on the next iteration. Fixing this requires separating metadata-only resolution from artifact download.
- **Platform installers need Java.** Forge and NeoForge installers require a `java` binary on PATH. The pipeline checks for this and fails fast if unavailable.
- **MCDR breaks the pattern.** MCDReforged's installation path is special-cased because its plugin system differs significantly from Java mod loaders.
- **The constraint solver is pure.** `MergeConstraintGraph` has zero I/O — it imports only `types`. Keep it that way. All probing, routing, logging, and output belong outside the solver boundary.
- **Workspace access is a singleton.** `workspace.ServerInfo()` is a cached singleton. The install pipeline reads it to determine server platform, version, and directory layout. Changes to the server (like installing a platform identity package) require `workspace.InvalidateServerInfo()` to refresh the cache.
