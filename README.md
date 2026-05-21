<div align="center">
  <img src="images/banner.png" alt="lucy banner" width="80%" />

  #### English | [中文](README_CN.md)

  <h2>
    <sub>Describe your server, not build it from scratch.</sub>
    <div>Manage your server with one unified CLI.</div>
  </h2>

  ### Lucy: The Modern Minecraft Server Environment Manager

  [![Build](https://github.com/mclucy/lucy/actions/workflows/build.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/build.yml) [![Tests](https://github.com/mclucy/lucy/actions/workflows/tests.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/tests.yml) [![Coverage](https://github.com/mclucy/lucy/wiki/dev/coverage.svg)](https://raw.githack.com/wiki/mclucy/lucy/dev/coverage.html) [![Go Report Card](https://goreportcard.com/badge/github.com/mclucy/lucy)](https://goreportcard.com/report/github.com/mclucy/lucy) [![License](https://img.shields.io/github/license/mclucy/lucy)](LICENSE) [![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/mclucy/lucy)

  ⭐️ If you like this project, please consider starring the repo!

  [Overview](#overview) • [Getting Started](#getting-started) • [Commands](#commands) • [Concepts](#concepts)
</div>

> [!IMPORTANT]
> This project is currently **incomplete** and under active development. Features and functionalities are subject to change. If you're interested in contributing or want to stay updated, please contact <4rcadia.0@gmail.com>, or join the [QQ groupchat](https://qm.qq.com/q/Sf65NVYaAi). A Discord server will be up soon.

## Overview

`lucy` is a server-aware environment manager for Minecraft servers. It does not assume a blank slate: it starts from the directory you already have, probes live files, detects the environment topology, and lets you decide the boundary Lucy should manage.

If you've used `apt`, `brew`, or `npm`, some commands will feel familiar. Lucy borrows the same ideas of intent, resolution, locks, and sync, then adapts them for messy Minecraft servers where manual jars, generated worlds, external tools, and managed content may legitimately coexist.

### Why Lucy?

Managing a Minecraft server means juggling mods, plugins, server cores, dependencies, and version compatibility across platforms like Fabric, Forge, NeoForge, and MCDR. Existing tools either require starting from scratch or lack awareness of what's already running. Lucy takes a different approach:

- **Server-aware from day one** — `lucy init` inspects your existing directory and adopts what's already there before asking what to manage.
- **Intent-based management** — declare what you want in a manifest; Lucy resolves exact versions, dependencies, and hashes into a reproducible lock file.
- **Coexists with manual changes** — managed and unmanaged content can live side by side. Lucy respects boundaries you set.
- **High-aesthetic CLI** — built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lip Gloss](https://github.com/charmbracelet/lipgloss) for a polished terminal experience.

### Design Philosophy

- Manage Minecraft server environments as stateful, semantic systems you can reason about, not just downloadable files.
- Model complex server state with a powerful topology model.
- Operators only need fuzzy intent (manifest); `lucy` resolves it into reproducible exact results (lock file).
- Ensure automated management does not interfere with manual management.
- CLI output with high aesthetic standards.
- Machine-readable output for CI/CD and other toolchain integrations.

## Getting Started

### Installation

> [!WARNING]
> Do not install before the first beta release unless you intend to test or contribute to the project. All data lost in production environments is your responsibility.

```bash
go install github.com/mclucy/lucy@latest
```

### Quick Start

```bash
mkdir my-server && cd my-server   # Create server directory
lucy init                         # Manage this server with lucy
lucy add neoforge/create-aeronautics@latest  # Install mods, dependencies auto-resolved
lucy status   # Check your server status
lucy run      # Boot the server
```

`lucy init` starts by looking at the current directory. If you point it at an existing server, it takes over from live facts first, then asks what should become managed intent and what should remain manual or unmanaged.

## Commands

`lucy` provides commands for managing server packages and auditing server environments. All examples are subject to change during development.

### `init` — Initialize lucy state

Inspect the current directory, aggregate existing server information, and create project-local state files for `lucy` to manage the environment deliberately.

```bash
lucy init
lucy init --yes --game-version 1.21.4
lucy init --conflict abort
```

`lucy init` creates `.lucy/config.toml`, `.lucy/manifest.toml`, and `.lucy/lock.json`.

For an existing server, `lucy init` behaves like a takeover flow: discover runtime and package facts first, then let you decide what `lucy` should keep in sync and what should stay outside its scope.

| Flag               | Description                                                           |
| ------------------ | --------------------------------------------------------------------- |
| `-y`, `--yes`      | Skip prompts and accept defaults                                      |
| `--game-version`   | Set the game version for non-interactive init                         |
| `-c`, `--conflict` | Choose `preserve`, `abort`, or `overwrite` for existing `.lucy` files |

### `add` — Add packages

Add mods, plugins, or server cores to manifest intent. `lucy add` resolves dependencies, accepts fuzzy versions, and refreshes the lockfile with exact resolved facts.

```bash
lucy add fabric-api
lucy add fabric/lithium@latest
lucy add mcdr/example-plugin@compatible
```

### `remove` — Remove packages

Remove packages from required intent and prune no-longer-needed transitive dependencies from the lock.

```bash
lucy remove fabric/lithium
```

### `install` — Sync managed runtime state

Apply the lockfile to the managed runtime scope. `lucy install` uses exact lockfile facts when they are current, and falls back to required intent if the lock is stale.

```bash
lucy install
```

### `search` — Find packages

Search across supported sources with filtering and sorting.

```bash
lucy search fabric/carpet
lucy search carpet --source modrinth --index downloads
```

| Flag             | Description                                      |
| ---------------- | ------------------------------------------------ |
| `-i`, `--index`  | Sort by `relevance`, `downloads`, or `newest`    |
| `-c`, `--client` | Include client-only mods                         |
| `-s`, `--source` | Restrict to a specific source (e.g., `modrinth`) |
| `-l`, `--long`   | Show hidden or collapsed output                  |

### `status` — Server environment overview

`lucy status` is a [`neofetch`](https://github.com/dylanaraps/neofetch)/[`fastfetch`](https://github.com/fastfetch-cli/fastfetch)-style overview for Minecraft server environments. It surfaces what `lucy` can detect, audit, and reason about in the current directory:

- Game version
- Server core
- Modding platform
- Detected environment topology
- Runtime activity and basic risk signals
- List of mods/plugins
- ...and more

### `info` — Package details

Get metadata, descriptions, and version history for a package.

```bash
lucy info fabric/fabric-api@latest --long
```

### `cache` — Manage download cache

List or clear the local download cache.

```bash
lucy cache ls
lucy cache clear
```

- `ls` — List cached entries (supports `--json` and `--no-style`)
- `clear` — Clear all cached downloads (supports `--no-style`)

### Global Flags

| Flag           | Description                                |
| -------------- | ------------------------------------------ |
| `--debug`      | Show debug logs                            |
| `--log-file`   | Output the path to the logfile             |
| `--print-logs` | Print logs to console                      |
| `--no-style`   | Disable colored and styled output globally |

---

## Concepts

### Core Definitions

A **platform** is the compatibility or runtime surface a package targets, such as Fabric, Forge, NeoForge, MCDR, or vanilla Minecraft. A **project** is a piece of software like a mod, plugin, or server-side extension. A **package** is a compiled, ready-to-use instance of a project with a specific platform and version — the thing you actually install. Together, these packages form the local server environment that `lucy` inspects, adopts, audits, and manages.

Not every platform plays the same role. For example, MCDR is an independent controller/plugin framework for managing Minecraft servers from the outside; it is not a Bukkit-derived plugin layer.

Managed intent lives in `.lucy/manifest.toml`. Exact resolved facts live in `.lucy/lock.json` and include versions, hashes, install paths, and provenance for the managed closure.

### Package Identifiers

Packages are identified using the format: `platform/project@version`

```text
fabric/fabric-api@1.2.3
   ↑       ↑        ↑
platform  name   version
```

All parts are optional except the project name. If you omit the platform, `lucy` infers it from the environment. The project is the name or ID of the mod/plugin. The version can be specific, `@latest`, or `@compatible` (the fuzzy default when you omit a version).

`latest` means newest available; `compatible` means newest version that appears to fit the inferred environment under available metadata (best-effort resolution).

**Supported platforms:** `fabric`, `forge`, `neoforge`, `mcdr`, `minecraft`, `none`

Some environments mix multiple compatible platforms at once. For example, a server can have a primary loader plus extra compatible layers that Lucy records in the manifest.

**Supported sources:** `modrinth`, `curseforge`, `github`, `mcdr`

> [!NOTE]
> Logo and axolotl pixel art are copyright Mojang AB. We are working on original replacements.
