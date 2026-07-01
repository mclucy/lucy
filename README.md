<div align="center">
  <img src="images/banner.png" alt="lucy banner" width="80%" />

#### English | [中文](README_CN.md)

### Lucy

<h3>
  <sup>The modern Minecraft server package manager</sup>
</h3>

[![CI](https://github.com/mclucy/lucy/actions/workflows/ci.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/ci.yml) [![Coverage](https://github.com/mclucy/lucy/wiki/badge/coverage.svg)](https://raw.githack.com/wiki/mclucy/lucy/dev/coverage.html) [![Go Report Card](https://goreportcard.com/badge/github.com/mclucy/lucy)](https://goreportcard.com/report/github.com/mclucy/lucy) [![License](https://img.shields.io/github/license/mclucy/lucy)](LICENSE) [![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/mclucy/lucy)
</div>

> [!WARNING]
> DeepWiki is currently stale

> [!IMPORTANT]
> This project is under active development and incomplete. Everything may change. Contact <4rcadia.0@gmail.com> or join the [QQ group](https://qm.qq.com/q/Sf65NVYaAi) to contribute or stay updated. \
> ⭐️ Star if you like this!

## Overview

Mange mods, plugins, and server components with one command.

```bash
cd your-server
lucy init                  # Set up Lucy in this workspace (server)
lucy add fabric            # Install fabric
lucy add lithium@latest    # Install mods
```

- Declare packages in the manifest. Lucy resolves exact versions and checksums. `lucy install` fetches and places them.
- `lucy init` discovers your runtime, platform, and installed packages, then asks what to manage. Everything else stays untouched.
- Lucy builds a graph of your runtime — Fabric, Forge, MCDR, Paper, Velocity — mapping roles, capabilities, and risk levels. This graph powers `lucy status`, init discovery, and compatibility resolution.

## Getting Started

> [!WARNING]
> We do not recommand using pre-beta versions in production environments.

```bash
go install github.com/mclucy/lucy@latest   # native
brew install --HEAD mclucy/tap/lucy        # homebrew
```

## Commands

### `lucy init`

Create manifest and lock file. Existing servers are respected.

```bash
lucy init
```

| Flag             | Description                                             |
| ---------------- | ------------------------------------------------------- |
| `-y`, `--yes`    | Skip prompts, accept defaults                           |
| `--game-version` | Game version for non-interactive init (default: `1.21`) |

### `lucy add`

Add anything to your server.

```bash
lucy add fabric-api
lucy add fabric/lithium@latest
lucy add folia
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
lucy search modrinth:carpet --index downloads --platform fabric
```

| Flag             | Description                                     |
| ---------------- | ----------------------------------------------- |
| `-i`, `--index`  | Sort: `relevance`, `downloads`, `newest`        |
| `-c`, `--client` | Include client-only mods                        |
| `--platform`     | Filter: `fabric`, `forge`, `neoforge`, `bukkit` |
| `-l`, `--long`   | Show full output                                            |
| `--json`         | Print raw JSON                                              |

### `lucy status`

Display what Lucy detects in the current directory: game version, server core, platform, topology, runtime activity, risk signals, and installed packages.

```bash
lucy status
lucy status --json --long
```

### `lucy topology`

Render the detected server runtime topology as an ASCII diagram.

```bash
lucy topology
lucy topology --long
lucy topology --json
```

| Flag           | Description                                              |
| -------------- | -------------------------------------------------------- |
| `-l`, `--long` | Show role, capabilities, and risk level inside each node |

### `lucy info`

Get metadata, description, authors, and version history for a package.

```bash
lucy info fabric/fabric-api@latest --long
```

| Flag           | Description |
| -------------- | ----------- |
| `-l`, `--long` | Full output |

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

### `lucy bisect`

```bash
lucy bisect start          # Start a binary-search session
lucy bisect good           # Mark current midpoint as good (bad mod is in right half)
lucy bisect bad            # Mark current midpoint as bad (bad mod is in left half)
lucy bisect status         # Show the active bisect session
lucy bisect reset          # Abort the session and re-enable mods
```

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
