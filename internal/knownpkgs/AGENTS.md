# internal/knownpkgs/

Local→remote identity bridge for the Lucy package manager.

ForEcosystem background on why Minecraft package identity is split across two authorities (remote slugs vs local loader IDs), see `types/AGENTS.md` § "The Identity Problem". This document covers how `knownpkgs` alleviates the bridging problem, not why it exists.

## The Problem

A user looks at their server's `mods/` directory and sees a mod. They run `lucy status` and see a name — the name the mod loader uses internally. They type that name into `lucy info`, `lucy rm`, or `lucy add`. But that name might not be what Modrinth or CurseForge calls it. The command fails or resolves to the wrong package.

Without a bridge between "what the server calls it" and "what the registry calls it," Lucy cannot reliably operate on packages the user can see but the registries know by a different name.

## What knownpkgs Does

`knownpkgs` is a persistent cache of verified local→remote name mappings. It remembers that the artifact on disk with local ID `AANobbMI` and a specific file hash is the same package that Modrinth calls `sodium`.

Mappings are verified by file hash — Lucy hashes the artifact, queries upstream providers ("do you recognize this file?"), and stores the answer. This makes the mapping trustworthy: it's not a name guess, it's a cryptographic match.

The cache is global (not per-server) and survives across sessions. When Lucy probes a server directory, it populates the cache as a side effect. Future sessions benefit from cached mappings without re-querying providers.

## How Resolution Should Work

When a user types an unscoped package name:

1. **Probe the current server** — hash installed artifacts, ask providers to identify them, cache the results
2. **Try the name as a remote slug** — query providers directly
3. **Fall back to cached mappings** — historical matches from previous probe sessions

Explicit source syntax (`modrinth:sodium`) bypasses the entire chain. The user said exactly what they mean; no guessing needed.

## Trust

Only hash-verified mappings are persisted. The store tracks how each mapping was resolved so that weaker tiers (URL extraction from artifact metadata, name heuristics) can be introduced later without silently degrading trust.

Two lookup modes exist: precise (exact source + local ID + file hash) and loose (source + local ID, any hash). Loose lookups handle version upgrades — same mod, new file, the remote slug hasn't changed.

## UX Goal

A user should be able to type what they see — whether that's a Modrinth slug from a README, a local mod ID from `lucy status`, or just a name someone told them in Discord — and Lucy resolves it correctly. `knownpkgs` is the machinery that makes this possible for the cases where the two naming systems disagree.
