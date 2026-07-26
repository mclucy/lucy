# types/

Pure domain types for the Lucy package manager. This package has **zero external dependencies** by design — it imports only `internal/fn` for generic helpers. All other Lucy packages depend on types, never the reverse.

All functions in this package must be deterministic and side-effect free: no logging, no filesystem access, no panics.

## The Identity Problem

Minecraft package identity is split across two authorities that both matter, but they do not share a universal naming system.

### Remote identity

Remote sources define human-facing, indexable names: Modrinth slugs, CurseForge project IDs/slugs, GitHub repos, future custom registries, maybe local directories. These names are useful for discovery, fetching, version resolution, dependency metadata, and sharing manifests. Remote identity is usually globally unique within its source.

### Local identity

Local artifacts define loader-facing names: Fabric `id`, Forge/NeoForge `modId`, Bukkit/Paper `plugin.yml` name, Velocity/Sponge/MCDR IDs, etc. These are the names the runtime actually enforces. Local identity is the truth for a functioning server, but only within that server context; it is not globally unique and may not match the remote slug.

### What this means for the type system

Lucy needs to reconcile two truths:

- **Remote identity** answers: "Where do I fetch this from, and what does the index call it?"
- **Local identity** answers: "What does the server actually load, conflict-check, and expose?"

Users interact with both without knowing which one they typed. A name from `lucy status` is probably local. A name from Modrinth, a README, or a manifest is probably remote. Sometimes they match; sometimes they do not. Private mods and custom sources make this worse.

The correct model is a mapping problem, not a mode problem:

- Remote/source-scoped refs are shareable, fetchable package identities.
- Local refs are observed runtime identities tied to a workspace/server.
- Bridges between them use the strongest evidence available: hash lookup first, metadata URL second, search/name heuristics last if allowed.
- Provenance/trust level is preserved so Lucy knows whether a mapping is verified or guessed.
- Explicit source syntax bypasses ambiguity; unscoped input is context-sensitive.

Without a working local→remote bridge, scoped syntax only gives users a way to be explicit, not a way for Lucy to understand what is already installed.

## Package Reference Hierarchy

The reference types form a hierarchy of increasing specificity:

- `BarePackageName` — just a name string (`fabric-api`)
- `PackageRef` — ecosystem + name (`fabric/fabric-api`); field `Eco`
- `VersionedPackageRef` — ecosystem + name + version (`fabric/fabric-api@0.100.0`)
- `ScopedPackageRef` — source + ecosystem + name (`modrinth:fabric/fabric-api`)
- `FullPackageRef` — source + ecosystem + name + version (`modrinth:fabric/fabric-api@0.100.0`)

`Ecosystem` (formerly **Platform** / `PlatformId`) is an enum: definite loaders (Fabric, Forge, NeoForge, MCDR, Bukkit, …), ambiguous (`EcoAny`), or structural (`EcoBare` — no mod loader; manifest may still use legacy `none`). Alias `EcoVanilla` = `EcoMinecraft`.

`SourceId` identifies which upstream provider a package comes from (Modrinth, CurseForge, GitHub, MCDR, Hangar, Spiget, etc.). `SourceAuto` means "let Lucy choose."

## Core Packages

Some packages represent a bootable server product (Minecraft, Fabric loader, Forge, Paper, hybrids, proxies). These are "core packages" — their installation means bootstrapping the server platform, not placing a jar in the mods directory. `type_core_package.go` defines the `CorePackage` enum and the alias catalog behind `NormalizeCorePackage`/`IsCorePackage`; install/bootstrap decides which cores are actually installable.

## Version System

Lucy supports multiple versioning schemes because the Minecraft ecosystem uses them all:

- `BareVersion` — raw version string, may be a special constant (`any`, `stable`, `beta`, `none`, `unknown`)
- `ResolvableVersion` — parsed, comparable version implementing `Compare(v2) (int, bool)` and `Scheme() VersionScheme`
- Supported schemes: `Semver`, `Maven`, `MinecraftSnapshot`, `MinecraftRelease`
- Cross-scheme comparison returns `(0, false)` — incomparable, not equal

## Dependencies

`Dependency` holds a version constraint expression in 2D DNF form: outer array is OR, inner is AND. Each sub-expression has a `VersionOperator` (eq, neq, gt, gte, lt, lte, plus weak variants `~` and `^` for semver) and a `ResolvableVersion` value. The `Embedded` field marks JarInJar dependencies that are satisfied without a separate file.

`PackageDependencies` wraps a `[]Dependency` slice with an `Authentic` flag: `true` means the dependency list was read from the actual artifact (jar metadata), `false` means it came from an upstream API and may be inaccurate.

## Gotchas

- `Dependency.Id.Version` is always empty — never read it. Only `Id.Eco` and `Id.Name` identify the dependency target; the constraint is in `Dependency.Constraint`.
- `EcoAny` is not a wildcard — it is ambiguous-but-single-valued and must reduce to a definite ecosystem during evaluation. Do not treat it as "matches everything."
- `Package` (in `type_package.go`) is a legacy composite struct. It bundles remote metadata, local install state, and dependencies into one type. It is being phased out in favor of lifecycle-specific types.
