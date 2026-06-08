<div align="center">
  <img src="images/banner.png" alt="lucy banner" width="80%" />

#### English | [中文](README_CN.md)

### Lucy

<h3>
  <sup>The modern Minecraft server package manager</sup>
</h3>

  [![Build](https://github.com/mclucy/lucy/actions/workflows/build.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/build.yml) [![Tests](https://github.com/mclucy/lucy/actions/workflows/tests.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/tests.yml) [![Coverage](https://github.com/mclucy/lucy/wiki/dev/coverage.svg)](https://raw.githack.com/wiki/mclucy/lucy/dev/coverage.html) [![Go Report Card](https://goreportcard.com/badge/github.com/mclucy/lucy)](https://goreportcard.com/report/github.com/mclucy/lucy) [![License](https://img.shields.io/github/license/mclucy/lucy)](LICENSE) [![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/mclucy/lucy)
</div>

> [!IMPORTANT]
> This project is under active development and incomplete. Everything may change. Contact <4rcadia.0@gmail.com> or join the [QQ group](https://qm.qq.com/q/Sf65NVYaAi) to contribute or stay updated. \
> ⭐️ If you like this project, star the repo!

## Overview

Declare the mods, plugins, and server cores you want. Lucy resolves exact versions and dependencies, writes a lock file, and keeps your managed scope in sync. Point it at an existing directory and it picks up from what's already there — or start fresh, either way.

```bash
cd your-server
lucy init                         # Set up Lucy in this directory
lucy add fabric/lithium@latest    # Resolve exact version + dependencies
lucy install                      # Sync managed scope from the lock file
```

- Declare packages in the manifest. Lucy resolves exact versions and checksums. `lucy install` fetches and places them.
- `lucy init` discovers your runtime, platform, and installed packages, then asks what to manage. Everything else stays untouched.
- Lucy builds a graph of your runtime — Fabric, Forge, MCDR, Paper, Velocity — mapping roles, capabilities, and risk levels. This graph powers `lucy status`, init discovery, and compatibility resolution.

## Getting Started

> [!WARNING]
> Do not install before the first beta unless you plan to test or contribute. Data loss is your responsibility.

```bash
go install github.com/mclucy/lucy@latest
```

```bash
mkdir my-server && cd my-server
lucy init                         # Take over this directory
lucy add fabric/fabric-api@latest # Add a mod — dependencies resolve automatically
lucy status                       # See what's detected
lucy install                      # Sync managed packages from the lock file
```

## Commands

### `lucy init`

Probe the directory, discover the server environment, and create state files.

```bash
lucy init
lucy init --yes --game-version 1.21.4
lucy init --conflict abort
```

Creates `.lucy/config.toml`, `.lucy/manifest.json`, and `.lucy/lock.json`.

| Flag               | Description                                             |
| ------------------ | ------------------------------------------------------- |
| `-y`, `--yes`      | Skip prompts, accept defaults                           |
| `--game-version`   | Game version for non-interactive init (default: `1.21`) |
| `-c`, `--conflict` | `preserve` (default), `abort`, or `overwrite`           |

### `lucy add`

Add mods, plugins, or server cores to the manifest. Lucy resolves exact versions and rewrites the lock file.

```bash
lucy add fabric-api
lucy add fabric/lithium@latest
lucy add mcdr/example-plugin@compatible
```

| Flag              | Description                                     |
| ----------------- | ----------------------------------------------- |
| `-f`, `--force`   | Skip version, dependency, and platform warnings |
| `--with-optional` | Include optional upstream dependencies          |
| `--no-optional`   | Skip optional dependencies (default)            |

### `lucy remove`

Remove packages from the manifest. Prunes unused transitive dependencies from the lock.

```bash
lucy remove fabric/lithium
```

### `lucy install`

Apply the lock file to the managed runtime. Uses exact lock data when current, falls back to manifest intent when stale.

```bash
lucy install
```

### `lucy search`

Search across sources with filtering and sorting.

```bash
lucy search fabric/carpet
lucy search carpet --source modrinth --index downloads --platform fabric
```

| Flag             | Description                                                 |
| ---------------- | ----------------------------------------------------------- |
| `-i`, `--index`  | Sort: `relevance`, `downloads`, `newest`                    |
| `-c`, `--client` | Include client-only mods                                    |
| `-s`, `--source` | Restrict source: `modrinth`, `curseforge`, `github`, `mcdr` |
| `--platform`     | Filter: `fabric`, `forge`, `neoforge`, `bukkit`             |
| `-l`, `--long`   | Show full output                                            |
| `--json`         | Print raw JSON                                              |

### `lucy status`

Display what Lucy detects in the current directory: game version, server core, platform, topology, runtime activity, risk signals, and installed packages.

```bash
lucy status
lucy status --json --long
```

### `lucy info`

Get metadata, description, authors, and version history for a package.

```bash
lucy info fabric/fabric-api@latest --long
```

| Flag             | Description    |
| ---------------- | -------------- |
| `-s`, `--source` | Specify source |
| `-l`, `--long`   | Full output    |
| `--json`         | Raw JSON       |

### `lucy tree`

Display the dependency tree.

```bash
lucy tree --live --depth 2
```

| Flag      | Description                               |
| --------- | ----------------------------------------- |
| `--live`  | Probe running server instead of lock file |
| `--depth` | Limit depth (0 = unlimited)               |
| `--json`  | Raw JSON                                  |

### `lucy leaves`

List packages with no dependents. Use this to find what's safe to remove.

```bash
lucy leaves --live
```

| Flag     | Description                               |
| -------- | ----------------------------------------- |
| `--live` | Probe running server instead of lock file |
| `--json` | Raw JSON                                  |

### `lucy cache`

```bash
lucy cache ls              # List cached downloads
lucy cache clear           # Clear all cached downloads
lucy cache slugs ls        # List slug-to-package-ID mappings
lucy cache slugs clear     # Clear slug mappings
```

| Subcommand    | Flags    |
| ------------- | -------- |
| `ls`, `list`  | `--json` |
| `clear`, `rm` |          |
| `slugs ls`    | `--json` |
| `slugs clear` |          |

### Stubs

Registered but not yet implemented:

| Command   | Planned                           |
| --------- | --------------------------------- |
| `doctor`  | Diagnose server environment risks |
| `export`  | Export config or generate client  |
| `upgrade` | Upgrade installed packages        |

### Global Flags

| Flag           | Description            |
| -------------- | ---------------------- |
| `--debug`      | Show debug logs        |
| `--log-file`   | Print path to logfile  |
| `--print-logs` | Print logs to console  |
| `--no-style`   | Disable colored output |

## Concepts

### Package Identifiers

```text
[platform/]name[@version]
```

Only the name is required. Omit the platform and Lucy infers it from the environment. Omit the version to get `@compatible` (newest match for your server).

```text
fabric/fabric-api@1.2.3
   ↑       ↑        ↑
platform  name   version
```

`@latest` is the newest available. `@compatible` is the default — best-effort match against the detected environment.

Platforms accepted in the manifest: `none`, `fabric`, `forge`, `neoforge`, `mcdr`

The type system also knows `bukkit`, `sponge`, `velocity`, and `bungeecord` for topology detection, but you can't set these as the primary platform yet.

Data sources: `modrinth`, `curseforge`, `github`, `mcdr` (`hangar` and `spiget` are defined but not yet wired into the resolver).

### State Files

Intent lives in `.lucy/manifest.json`. Resolved facts (versions, hashes, install paths, provenance) live in `.lucy/lock.json`. Config lives in `.lucy/config.toml`.

### Runtime Topology

Lucy builds a graph of your server's runtime. Each node (Fabric, Forge, Paper, MCDR, Geyser, Velocity) carries a role, a set of capabilities (`fabric_mods`, `bukkit_plugins`, `mcdr_plugins`), and a risk level. Edges describe how nodes relate: one adapts another, one bridges to another. This graph powers `lucy status`, init discovery, and compatibility resolution.

> [!NOTE]
> Logo and axolotl pixel art are copyright Mojang AB. Original replacements in progress.
