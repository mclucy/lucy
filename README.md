<div align="center">
  <img src="images/banner.png" alt="banner" width="80%" />

#### English | [中文](README_CN.md)

  <h2>
    <sub>Describe your server, not build it from scratch.</sub>
    <div>Manage your server with one unified CLI.</div>
  </h2>

### Lucy: The Modern Minecraft Server Environment Manager

  [![Build](https://github.com/mclucy/lucy/actions/workflows/build.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/build.yml)
  [![Tests](https://github.com/mclucy/lucy/actions/workflows/tests.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/tests.yml)
  [![Coverage](https://github.com/mclucy/lucy/wiki/dev/coverage.svg)](https://raw.githack.com/wiki/mclucy/lucy/dev/coverage.html)
  [![Go Report Card](https://goreportcard.com/badge/github.com/mclucy/lucy)](https://goreportcard.com/report/github.com/mclucy/lucy)
  [![Last Commit](https://img.shields.io/github/last-commit/mclucy/lucy)](https://github.com/mclucy/lucy/commits/main/)
  [![Code Size](https://img.shields.io/github/languages/code-size/mclucy/lucy)](main.go)
  [![License](https://img.shields.io/github/license/mclucy/lucy)](LICENSE)
  [![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/mclucy/lucy)

</div>

> [!IMPORTANT]
> This project is currently **INCOMPLETE** and under active development. Features and functionalities are subject to change.
> If you're interested in contributing or want to stay updated, please contact <4rcadia.0@gmail.com>, or join the [QQ groupchat](https://qm.qq.com/q/Sf65NVYaAi). A Discord server will be up soon!

## Overview

`lucy` is a server-aware environment manager for Minecraft servers. It starts by taking over the server you already run, inspects what is actually installed, and then helps you define the scope Lucy should manage. It keeps that managed scope explicit with soft manifest intent and exact lockfile facts, while manual or unmanaged content stays part of the server and outside Lucy's ownership.

If you've used `apt`, `brew`, or `npm`, some commands will feel familiar. The difference is that `lucy` starts from the server you already run. It does not assume a blank slate, and it does not try to replace everything on disk. Manual and managed content can coexist, and Lucy only claims the parts you place in its scope.

### Core Features

<!-- TODO: Replace this section with .gif demo -->

- Automatic dependency resolution and conflict handling
- Package access from Modrinth, CurseForge, MCDR Plugin Catalog, and more...
- Discovery-led server probing and environment inference
- Take over an existing server before trying to reshape it
- Keep manual and unmanaged content alongside the managed scope
- Keep manifest intent soft while recording exact lockfile facts
- Topology-aware status reporting and risk surfacing
- Non-intrusive design, all operations are independent of server runtime
- Shell completion for bash, zsh, fish, and pwsh
- Beautiful CLI output
- Machine-readable output formats for CI/CD pipelines and shell scripts

## 🚀 Getting Started

### Installation

> [!WARNING]
> Do not install before the first beta release unless you intend to test or contribute to the project. All data lost in production environments is your responsibility.

```bash
go install github.com/mclucy/lucy@latest
```

### Quick Start

```bash
mkdir my-server && cd my-server # Create server directory
lucy init # Manage this server with lucy
lucy add neoforge/create-aeronautics@latest # Install mods, dependencies are automatically resolved
lucy status # Check your server status
lucy run # Boot the server
```

`lucy init` starts by looking at the current directory. If you point it at an existing server, it takes over from live facts first, then asks what should become managed intent and what should remain manual or unmanaged.

---

## 🛠️ Commands

`lucy` provides commands for managing server packages and auditing server environments. All examples are subject to change during development.

### `init` - Initialize lucy state

Inspect the current directory, aggregate existing server information, and create project-local state files for `lucy` to manage the environment deliberately.

```bash
lucy init
lucy init --yes --game-version 1.21.4
lucy init --conflict abort
```

`lucy init` creates `.lucy/config.toml`, `.lucy/manifest.toml`, and `.lucy/lock.json`.

For an existing server, `lucy init` behaves like a takeover flow: discover runtime and package facts first, then let you decide what `lucy` should keep in sync and what should stay outside its scope.

- `-y`, `--yes`: Skip prompts and accept defaults
- `--game-version`: Set the game version for non-interactive init
- `-c`, `--conflict`: Choose `preserve`, `abort`, or `overwrite` for existing `.lucy` files

### `add` - Add packages

Add mods, plugins, or server cores to manifest intent. `lucy add` resolves dependencies, accepts fuzzy versions, and refreshes the lockfile with exact resolved facts.

```bash
lucy add fabric-api
lucy add fabric/lithium@latest
lucy add mcdr/example-plugin@compatible
```

### `remove` - Remove packages

Remove packages from required intent and prune no-longer-needed transitive dependencies from the lock.

```bash
lucy remove fabric/lithium
```

### `install` - Sync managed runtime state

Apply the lockfile to the managed runtime scope. `lucy install` uses exact lockfile facts when they are current, and falls back to required intent if the lock is stale.

```bash
lucy install
```

### `search` - Find packages

Search across supported sources with filtering and sorting.

```bash
lucy search fabric/carpet
lucy search carpet --source modrinth --index downloads
```

- `-i`, `--index`: Sort by `relevance`, `downloads`, or `newest`
- `-c`, `--client`: Include client-only mods
- `-s`, `--source`: Restrict to a specific source (e.g., `modrinth`)
- `-l`, `--long`: Show hidden or collapsed output

### `status` - Server environment overview

`lucy status` is a [`neofetch`](https://github.com/dylanaraps/neofetch)/[`fastfetch`](https://github.com/fastfetch-cli/fastfetch)-style overview for Minecraft server environments. It is designed to surface what `lucy` can detect, audit, and reason about in the current directory:

- Game version
- Server core
- Modding platform
- Detected environment topology
- Runtime activity and basic risk signals
- List of mods/plugins
- ...and more

<!-- TODO: Add screenshot -->

### `info` - Package details

Get metadata, descriptions, and version history for a package.

```bash
lucy info fabric/fabric-api@latest --long
```

<!-- TODO: Add screenshot -->

### `cache` - Manage download cache

List or clear the local download cache.

```bash
lucy cache ls
lucy cache clear
```

`ls` - List cached entries (supports `--json` and `--no-style`)
`clear` - Clear all cached downloads (supports `--no-style`)

### Global Flags

- `--debug`: Show debug logs
- `--log-file`: Output the path to the logfile
- `--print-logs`: Print logs to console
- `--no-style`: Disable colored and styled output globally

---

## 📖 Syntax & Concepts

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

Supported platforms: `fabric`, `forge`, `neoforge`, `mcdr`, `minecraft`, `none`

Some environments mix multiple compatible platforms at once. For example, a server can have a primary loader plus extra compatible layers that Lucy records in the manifest.

Supported sources: `modrinth`, `curseforge`, `github`, `mcdr`

---

## ⚖️ License

This project is licensed under the [Apache 2.0 License](LICENSE).

*Logo and axolotl pixel art are copyright Mojang AB. We are working on original replacements.*
