<div align="center">

  <img src="images/banner.png" alt="banner" width="80%" />

  <a href="README.md">English</a> | <a href="README_CN.md">中文</a>

  <h2>
    <sub>Describe your server, not build it from scratch.</sub>
    <div>Manage your Minecraft server with a unified CLI.</div>
  </h2>

  <h3>Lucy: The Modern Minecraft Server Environment Manager</h3>

  <img
    src="https://goreportcard.com/badge/github.com/mclucy/lucy"
    alt="Go Report Card"
  />
  <img
    src="https://github.com/mclucy/lucy/actions/workflows/github-code-scanning/codeql/badge.svg"
    alt="CodeQL"
  />
  <img
    src="https://img.shields.io/github/last-commit/mclucy/lucy"
    alt="Last Commit"
  />
  <img
    src="https://img.shields.io/github/languages/code-size/mclucy/lucy"
    alt="Code Size"
  />
  <img
    src="https://img.shields.io/github/license/mclucy/lucy"
    alt="License"
  />
  <a href="https://deepwiki.com/mclucy/lucy">
    <img src="https://deepwiki.com/badge.svg" alt="Ask DeepWiki">
  </a>

</div>

> [!IMPORTANT]
> This project is currently **INCOMPLETE** and under active development. Features and functionalities are subject to change.
> If you're interested in contributing or want to stay updated, please contact <4rcadia.0@gmail.com>, or join the [QQ groupchat](https://qm.qq.com/q/Sf65NVYaAi). A Discord server will be up soon!

## Overview

`lucy` is a server-aware environment manager for Minecraft servers. It helps you inspect an existing server, understand what is actually installed, and decide what part of that environment you want `lucy` to manage. It handles dependency resolution, version tracking, source routing, environment probing, and risk-aware visibility through a unified CLI.

If you've used `apt`, `brew`, or `npm`, some commands will feel familiar. The difference is that `lucy` starts from the server you already run. It does not assume a blank slate, and it does not require you to give up manual control over everything. It is meant to help server administrators get a messy real-world environment under control, one step at a time.

### Core Features

<!-- TODO: Replace this section with .gif demo -->

- Automatic dependency resolution and conflict handling
- Package access from Modrinth, CurseForge, MCDR Plugin Catalog, and more...
- Discovery-led server probing and environment inference
- Take over an existing server before trying to reshape it
- Keep track of what `lucy` manages without claiming the whole server
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
mkdir my-server && cd my-server
lucy init
lucy add fabric@latest
lucy add fabric/lithium@compatible
lucy status
java -jar fabric-server.jar
```

`lucy init` starts by looking at the current directory. If you point it at an existing server, it should help you understand what is already there before you decide what `lucy` should manage.

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

For an existing server, `lucy init` should behave like a takeover flow: discover runtime and package facts first, then let you decide what `lucy` should keep in sync and what should stay outside its scope.

- `-y`, `--yes`: Skip prompts and accept defaults
- `--game-version`: Set the game version for non-interactive init
- `-c`, `--conflict`: Choose `preserve`, `abort`, or `overwrite` for existing `.lucy` files

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

### `add` - Install packages

Add mods, plugins, or server cores. `lucy` resolves dependencies, verifies platform compatibility, and updates the local environment with minimal intrusion. Over time, `add` is meant to become part of how `lucy` records and maintains managed intent, not just how it drops files into a folder.

```bash
lucy add fabric/fabric-api@latest
lucy add neoforge/create --force
```

<!-- TODO: Add screenshot -->

### `status` - Server environment overview

`lucy status` is a `neofetch`-style overview for Minecraft server environments. It is designed to surface what `lucy` can detect, audit, and reason about in the current directory:

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

Not every platform plays the same role. For example, MCDR is an independent plugin framework that manages Minecraft servers from the outside; it is not a Bukkit-derived plugin layer.

### Package Identifiers

Packages are identified using the format: `platform/project@version`

```text
fabric/fabric-api@1.2.3
   ↑       ↑        ↑
platform  name   version
```

All parts are optional except the project name. If you omit the platform, `lucy` infers it from the environment. The project is the name or ID of the mod/plugin. The version can be specific, `@latest`, or `@compatible` (default).

Supported platforms: `fabric`, `forge`, `neoforge`, `mcdr`, `minecraft`, `none`

Supported sources: `modrinth`, `curseforge`, `github`, `mcdr`

---

## ⚖️ License

This project is licensed under the [Apache 2.0 License](LICENSE).

*Logo and axolotl pixel art are copyright Mojang AB. We are working on original replacements.*
