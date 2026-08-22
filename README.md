<div align="center">
  <img src="images/banner.png" alt="lucy banner" width="80%" />

#### English | [中文](README_CN.md)

### Lucy

Minecraft server package manager.

[![CI](https://github.com/mclucy/lucy/actions/workflows/ci.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/ci.yml) [![Coverage](https://github.com/mclucy/lucy/wiki/badge/coverage.svg)](https://raw.githack.com/wiki/mclucy/lucy/dev/coverage.html) [![License](https://img.shields.io/github/license/mclucy/lucy)](LICENSE) [![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/mclucy/lucy)
</div>

> [!WARNING]
> DeepWiki is currently stale.

> [!IMPORTANT]
> This project is under active development and is incomplete. APIs and behavior may change. Contact <4rcadiaaa@gmail.com> or join the [QQ group](https://qm.qq.com/q/Sf65NVYaAi) to contribute or follow updates.

## Overview

Manage Minecraft server components from any ecosystem with `npm`-like experience.

- Fabric
- Forge
- Neoforge
- Spigot/Paper Plugins
- MCDReforged

## Installation

> [!WARNING]
> Pre-beta versions are not recommended for production environments.

Install with Go:

```bash
go install github.com/mclucy/lucy@latest
```

Install with Homebrew:

```bash
brew install --HEAD mclucy/tap/lucy
```

## Quick start

```bash
mkdir my-server && cd my-server
lucy init                         # Initialize Lucy in this directory
lucy add fabric/fabric-api@stable # Add a mod; dependencies resolve automatically
lucy status                       # Show what Lucy detected
lucy install                      # Install packages from the lock file
```

## Commands

### Managing packages

#### `lucy init`

Creates the manifest and lock file in the current directory. Existing servers are preserved.

```bash
lucy init
```

| Flag | Description |
| ---------------- | ------------------------------------------------------- |
| `-y`, `--yes` | Skip prompts and accept defaults |
| `--game-version` | Game version for non-interactive init (default: `1.21`) |

#### `lucy add`

Adds a package to the manifest.

```bash
lucy add fabric-api
lucy add fabric/lithium@stable
lucy add folia
lucy add mcdr/example-plugin@beta
```

| Flag | Description |
| ----------------- | ----------------------------------------------- |
| `-f`, `--force` | Skip version, dependency, and platform warnings |
| `--with-optional` | Include optional upstream dependencies |
| `--no-optional` | Skip optional dependencies (default) |

#### `lucy remove`

Removes a package from the manifest and prunes unused transitive dependencies from the lock file.

```bash
lucy remove fabric/lithium
```

#### `lucy install`

Installs packages from the lock file. When the lock file is current, Lucy uses its exact data. When it is stale, Lucy falls back to the manifest.

```bash
lucy install
```

### Inspecting the workspace

#### `lucy status`

Shows what Lucy detects in the current directory: game version, server core, platform, runtime activity, risk signals, and installed packages.

```bash
lucy status
lucy status --json --long
```

#### `lucy search`

Searches across data sources.

```bash
lucy search fabric/carpet
lucy search modrinth:carpet --index downloads --platform fabric
```

| Flag | Description |
| ---------------- | ------------------------------------------------- |
| `-i`, `--index` | Sort by `relevance`, `downloads`, or `newest` |
| `-c`, `--client` | Include client-only mods |
| `--platform` | Filter by `fabric`, `forge`, `neoforge`, `bukkit` |
| `-l`, `--long` | Show full output |
| `--json` | Print raw JSON |

#### `lucy info`

Shows metadata, description, authors, and version history for a package.

```bash
lucy info fabric/fabric-api@stable --long
```

| Flag | Description |
| -------------- | ----------- |
| `-l`, `--long` | Full output |

#### `lucy tree`

Shows the dependency tree.

```bash
lucy tree --live --depth 2
```

| Flag | Description |
| --------- | ------------------------------------------------- |
| `--live` | Probe the running server instead of the lock file |
| `--depth` | Limit depth (0 = unlimited) |
| `--json` | Print raw JSON |

#### `lucy leaves`

Lists packages with no dependents. Use this command to find packages that are safe to remove.

```bash
lucy leaves --live
```

| Flag | Description |
| -------- | ------------------------------------------------- |
| `--live` | Probe the running server instead of the lock file |
| `--json` | Print raw JSON |

### Cache

#### `lucy cache`

Manages the local download cache.

```bash
lucy cache ls              # List cached downloads
lucy cache clear           # Clear all cached downloads
lucy cache slugs ls        # List slug-to-package-ID mappings
lucy cache slugs clear     # Clear slug mappings
```

| Subcommand | Flags |
| ------------- | -------- |
| `ls`, `list` | `--json` |
| `clear`, `rm` | |
| `slugs ls` | `--json` |
| `slugs clear` | |

### Troubleshooting

#### `lucy bisect`

Runs a binary search over installed mods to find a faulty mod.

```bash
lucy bisect start          # Start a binary-search session
lucy bisect good           # Mark current midpoint as good (bad mod is in the right half)
lucy bisect bad            # Mark current midpoint as bad (bad mod is in the left half)
lucy bisect status         # Show the active bisect session
lucy bisect reset          # Abort the session and re-enable mods
```

### Planned commands

The following commands are registered but not yet implemented.

| Command | Planned |
| --------- | ---------------------------------- |
| `doctor` | Diagnose server environment risks |
| `export` | Export config or generate a client |
| `upgrade` | Upgrade installed packages |

### Global flags

| Flag | Description |
| --------------- | ---------------------------------------- |
| `--debug` | Show debug logs |
| `--log-file` | Print path to logfile |
| `--print-logs` | Print logs to console |
| `--no-style` | Disable colored output |
| `--json-compact` | Print JSON output without indentation |

> [!NOTE]
> The logo and axolotl pixel art are copyright Mojang AB. Original replacements are in progress.
