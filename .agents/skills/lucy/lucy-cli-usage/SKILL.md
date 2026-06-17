---
name: lucy-cli-usage
description: For agents operating Lucy against a live Minecraft server directory. This is NOT a code review or debugging skill. For development debugging, see `debugging-lucy-cli`.

---



# Lucy CLI Usage Guide

> **For agents operating Lucy against a live Minecraft server directory.**
> This is NOT a code review or debugging skill. For development debugging, see `debugging-lucy-cli`.

## Important: Project Maturity Warning

Lucy is **pre-beta, under active development**. Expect:

- Some commands are stubs (defined but do nothing yet)
- Upstream provider behavior may change between builds
- Error messages may be unhelpful or missing for edge cases
- The state file schema is stabilizing but not frozen

**When something fails repeatedly**: report the failure to the user rather than retrying indefinitely. The feature may simply not be wired yet. Check the "Known Limitations" section below before assuming you're doing something wrong.

---

## Prerequisites

- A built `lucy` binary (see Taskfile: `task build` produces `dist/lucy-darwin-arm64-dev`)
- A Minecraft server directory (contains `server.jar` or platform-equivalent)
- For CurseForge results: the binary must be built with cipher keys (`task cipher-generate` requires `CF_API_KEY`)

---

## CLI Structure

```
lucy
├── init                    # Initialize Lucy state in a server directory
├── add <pkg>...            # Add packages to manifest + resolve + install
├── remove <pkg>...         # Remove packages from manifest + prune lock
├── install                 # Sync managed runtime from lockfile
├── search <query>          # Search for mods/plugins across sources
├── status                  # Display server runtime info
├── info <pkg>              # Display package metadata
├── tree                    # Display dependency tree
├── leaves                  # List leaf packages (safe to remove)
├── cache                   # Manage download cache
│   ├── ls                  # List cached entries
│   ├── clear               # Clear all cached downloads
│   └── slugs               # Manage slug resolution cache
│       ├── ls              # List slug-to-package-ID mappings
│       └── clear           # Clear slug mappings
├── doctor                  # [STUB] Diagnose server environment risks
├── export                  # [STUB] Export config or generate client
└── upgrade                 # [STUB] Upgrade installed packages
```

---

## Package Identifier Format

```
[platform/]name[@version]
```

| Component | Required | Examples | Notes |
|-----------|----------|----------|-------|
| platform | No | `fabric/`, `forge/`, `neoforge/`, `mcdr/` | Inferred from server environment if omitted |
| name | Yes | `lithium`, `fabric-api`, `sodium` | Slug or project name |
| version | No | `@latest`, `@compatible`, `@0.12.1` | Defaults to `@compatible` |

**Version selectors:**
- `@compatible` — newest version matching your server's game version and platform
- `@latest` — newest available regardless of compatibility (use with caution)
- `@1.2.3` — exact version pin

**Examples:**
```bash
lucy add lithium                    # Infer platform, use @compatible
lucy add fabric/sodium@latest       # Explicit platform, latest version
lucy add neoforge/connector         # NeoForge mod, compatible version
```

---

## Typical Workflow

### 1. Initialize a Server Directory

```bash
cd ~/minecraft-server
lucy init
```

Interactive TUI walks through: game version, platform selection, platform version, compatible platforms, package classification (which existing mods to manage).

**Non-interactive:**
```bash
lucy init --yes --game-version 1.21.4
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--yes` | `-y` | false | Accept defaults, skip TUI |
| `--conflict` | `-c` | `preserve` | `preserve` / `abort` / `overwrite` |
| `--game-version` | | `1.21` | Game version (non-interactive mode) |

**Conflict modes:**
- `preserve` — keep existing state files, only write missing ones
- `abort` — refuse if any state file already exists
- `overwrite` — replace all state files

### 2. Add Packages

```bash
lucy add fabric/lithium
lucy add fabric/fabric-api fabric/sodium    # Multiple at once
lucy add --force neoforge/some-mod@0.5.0    # Skip warnings
```

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--force` | `-f` | false | Ignore version/dependency/platform warnings |
| `--with-optional` | | false | Include optional upstream dependencies |
| `--no-optional` | | false | Skip optional deps (this is default behavior) |

`add` resolves versions, downloads packages, installs them, and updates both `lucy.yaml` and `lucy-lock.yaml`.

### 3. Install (Sync from Lockfile)

```bash
lucy install
```

No arguments. Reads the lock and ensures the managed runtime matches it. Two paths:
- **Fast path**: lock fingerprint matches manifest — uses exact cached URLs/hashes
- **Re-resolution**: manifest changed since last lock — queries upstream sources

Idempotent: running twice with no changes is a no-op.

### 4. Remove Packages

```bash
lucy remove fabric/sodium
```

Updates the manifest (marks package removed) and prunes transitive deps from the lock. **Does not delete installed files from disk** — that behavior is not yet implemented.

### 5. Inspect Server State

```bash
lucy status              # Game version, platform, topology, risk, package lists
lucy status --json       # Machine-readable output
lucy status --long       # Full package IDs and paths

lucy tree                # Dependency tree from lock
lucy tree --live         # Probe running server instead of lock
lucy tree --depth 2      # Limit depth

lucy leaves              # Packages with no dependents (safe removal candidates)
```

### 6. Search and Discover

```bash
lucy search carpet                              # Search all sources
lucy search sodium --source modrinth            # Restrict to Modrinth
lucy search essentials --platform bukkit         # Filter by platform
lucy search worldedit --index downloads         # Sort by download count

lucy info fabric/lithium                        # Package details
lucy info fabric/lithium --long --source modrinth
```

**Search flags:**
| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--source` | `-s` | all | `modrinth`, `curseforge`, `github`, `mcdr` |
| `--platform` | | inferred | `fabric`, `forge`, `neoforge`, `bukkit` |
| `--index` | `-i` | `relevance` | `relevance`, `downloads`, `newest` |
| `--client` | `-c` | false | Also show client-only mods |
| `--json` | | false | Raw JSON output |
| `--long` | `-l` | false | Full output |

### 7. Cache Management

```bash
lucy cache ls            # List cached downloads
lucy cache clear         # Clear download cache
lucy cache slugs ls      # List slug resolution mappings
lucy cache slugs clear   # Clear slug cache
```

---

## Global Flags

Available on every command:

| Flag | Description |
|------|-------------|
| `--debug` | Show debug-level logs |
| `--log-file` | Output the path to the log file |
| `--print-logs` | Print logs to console |
| `--no-style` | Disable colored/styled output |

---

## State Files

Lucy creates two files in the server directory:

| File | Purpose | Written by |
|------|---------|------------|
| `lucy.yaml` | Manifest — declared package intent + optional config | `init`, `add`, `remove` |
| `lucy-lock.yaml` | Lock — exact resolved versions, URLs, hashes | `init`, `add`, `install` |

**Global config** lives at `~/.config/lucy/config.yaml` (macOS: `~/Library/Application Support/lucy/config.yaml`). Workspace config in `lucy.yaml` overrides global.

### Manifest Structure (lucy.yaml)

Key sections:
- `environment` — game version, platform, platform version, compatible platforms
- `packages[]` — each with `id` (platform/name), `version`, `source`, `role`, `side`
- `bundles[]` — managed non-package artifacts (config files, datapacks)
- `config` — optional source priority and upgrade mode overrides

Package roles: `required` (user-chosen), `transitive` (resolver-derived), `ignored` (not managed)

### Lock Structure (lucy-lock.yaml)

Each locked package has:
- Exact version, download URL, filename
- Content hash (sha512 or sha1) for integrity verification
- `install_path` — relative path where the file should live
- `provenance[]` — dependency chain explaining how it got resolved
- `requester` — which package caused this to be pulled in (`root` = user-requested)

The lock includes a `manifest_fingerprint` (SHA-256 of the serialized manifest). When this matches, `lucy install` trusts the lock without re-resolving.

---

## Supported Platforms

| Platform | As primary | As compatible | Package ecosystem |
|----------|-----------|---------------|-------------------|
| `fabric` | Yes | Yes | Fabric mods |
| `forge` | Yes | Yes | Forge mods |
| `neoforge` | Yes | Yes | NeoForge mods |
| `mcdr` | Yes | Yes | MCDR plugins |
| `bukkit` | No | No | Bukkit/Paper/Spigot plugins |
| `sponge` | No | No | Sponge plugins |
| `velocity` | No | No | Velocity plugins |
| `bungeecord` | No | No | BungeeCord plugins |

`none` = vanilla server with no modding platform.

## Data Sources

| Source | Status | Notes |
|--------|--------|-------|
| `modrinth` | Working | Primary source, usually best metadata |
| `curseforge` | Working | Requires cipher key at build time |
| `github` | Working | For GitHub Releases-based packages |
| `mcdr` | Working | MCDReforged plugin repository |
| `hangar` | Defined, not wired | May not return results |
| `spiget` | Defined, not wired | May not return results |

---

## Known Limitations & Incomplete Features

### Commands That Do Nothing Yet

These are registered but have no implementation (`RunE: nil`). They will print help text but perform no action:

- **`lucy doctor`** — intended to diagnose server environment risks. Not yet functional.
- **`lucy export`** — intended to export server config or generate a client modpack. Not yet functional.
- **`lucy upgrade`** — intended to upgrade installed packages to newer versions. Not yet functional.
- **`lucy config`** — intended to manage Lucy configuration. Defined but not even registered in the command tree (completely hidden from users).

### Partial or Evolving Behaviors

- **`lucy remove` does not delete files** — it updates the manifest and lock but leaves installed JARs on disk. The user (or a future `lucy install --prune` or `lucy doctor`) would need to handle cleanup.
- **Hangar and Spiget sources** are defined in the source enum but not wired into the resolver. Specifying `--source hangar` or `--source spiget` may produce errors or empty results.
- **Version constraint resolution** is functional but the constraint language (semver, maven ranges, minecraft snapshot format) may have edge cases that aren't fully handled.
- **Platform inference** works for common setups but exotic combinations (e.g., Sinytra Connector on NeoForge with Fabric mods) may produce unexpected results.
- **The `--force` flag on `add`** bypasses warnings but doesn't guarantee the package will work. It may install incompatible versions that crash the server.
- **Bundle management** (config files, datapacks, resourcepacks) is defined in the manifest schema but the install pipeline's handling of bundles may be incomplete.
- **Shell completions** work for package names via provider pattern, but completion quality depends on cache state and network availability.

### When Things Fail

If a command produces an unexpected error or hangs:

1. **Check if it's a stub** — `doctor`, `export`, `upgrade` do nothing. Don't retry them.
2. **Try `--debug`** — adds verbose logging that may clarify the failure.
3. **Check network** — `add`, `install`, `search`, `info` all require network access to upstream sources.
4. **Check state files** — if `lucy.yaml` or `lucy-lock.yaml` are malformed or from an older schema version, commands may fail. Re-running `lucy init --conflict overwrite` resets state.
5. **CurseForge failures** — if the binary wasn't built with cipher keys, CurseForge source will not work. Modrinth should still function.
6. **Report to user** — if a command fails after reasonable attempts and isn't listed as a stub, tell the user. The feature may have a bug or be mid-development.

---

## Non-Interactive Usage (for automation)

For scripted or agent-driven usage without TUI prompts:

```bash
# Initialize without interaction
lucy init --yes --game-version 1.21.4

# Add packages (no confirmation needed)
lucy add fabric/lithium fabric/sodium

# Sync
lucy install

# Machine-readable output
lucy status --json
lucy tree --json
lucy search sodium --json
lucy cache ls --json
```

The `--no-style` flag disables ANSI color codes for clean parsing of text output.

---

## Common Patterns

### Fresh Server Setup
```bash
mkdir my-server && cd my-server
# Place server.jar (or fabric-server-launch.jar, etc.) here first
lucy init --yes --game-version 1.21.4
lucy add fabric/fabric-api fabric/lithium fabric/sodium
lucy status
```

### Take Over Existing Server
```bash
cd /path/to/existing/server
lucy init
# TUI will detect existing mods and ask which to manage
lucy status --long
```

### Check What's Installed
```bash
lucy status           # Overview
lucy tree             # Full dependency graph
lucy leaves           # What has no dependents
```

### Search Before Adding
```bash
lucy search "world edit" --platform fabric --index downloads
lucy info fabric/worldedit --long
lucy add fabric/worldedit
```

---

## Gotchas for Agents

1. **Always run from the server directory** — Lucy operates on the current working directory (or the directory containing `lucy.yaml`). Running from the wrong directory will either fail or create state in the wrong place.

2. **`add` both resolves AND installs** — unlike some package managers where `add` only updates the manifest and `install` does the actual work, Lucy's `add` does both. Use `add` to get packages, use `install` to re-sync if something got out of alignment.

3. **`remove` does NOT delete files** — it only updates state files. The JARs remain on disk.

4. **Don't edit state files manually** — use the CLI commands. Manual edits may break the manifest fingerprint or produce invalid YAML that Lucy can't parse.

5. **Stubs exist in the command tree** — `doctor`, `export`, `upgrade` are visible in help but do nothing. Don't try to use them or wonder why they produce no output.

6. **Platform inference depends on server detection** — if the server directory doesn't have recognizable platform files (e.g., no `fabric-server-launch.jar`), Lucy may not infer the platform correctly. Use explicit `platform/name` format in that case.

7. **The lock is authoritative** — `lucy install` trusts the lock when the fingerprint matches. If you need to force re-resolution, modify the manifest (even trivially) to invalidate the fingerprint, then run `install`.

8. **`@compatible` vs `@latest`** — `@compatible` filters by your game version and platform compatibility. `@latest` grabs the newest version regardless. Prefer `@compatible` unless you know what you're doing.
