<div align="center">

  <img src="images/banner.png" alt="banner" width="80%" />

  <a href="README.md">English</a> | <a href="README_CN.md">中文</a>

  <h2>
    <sub>Servers. Clusters. Modpacks.</sub>
    <div>All in one command.</div>
  </h2>

  <h3>Lucy: The Modern Minecraft Server Package Manager</h3>

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

</div>

> [!WARNING]
> This project is currently **INCOMPLETE** and under active development. Features and functionalities are subject to change.
> If you're interested in contributing or want to stay updated, please contact <4rcadia.0@gmail.com>, or join the [QQ groupchat](https://qm.qq.com/q/Sf65NVYaAi). A Discord server will be up soon!

## Overview

`lucy` is a package manager for Minecraft servers. It handles dependency resolution, version tracking, and multi-source package access through a unified CLI. If you've used `apt`, `brew`, or `npm`, the workflow will feel familiar.

### Core Features

<!-- TODO: Replace this section with .gif demo -->

- Automatic dependency resolution and conflict handling
- Package access from Modrinth, CurseForge, MCDR Plugin Catalog, and more...
- Non-intrusive design, all operations are independent of server runtime
- Shell completion for bash, zsh, fish, and pwsh
- Beautiful CLI output
- Build to integrate into CI/CD pipelines and shell scripts via machine-readable output formats

## 🚀 Getting Started

### Installation

> [!WARNING]
> Do not install before the first beta release unless you intend to test or contribute to the code.

```bash
go install github.com/mclucy/lucy@latest
```

### Quick Start

```bash
mkdir my-server && cd my-server
lucy add fabric@latest
lucy add fabric/lithium@compatible
lucy status
java -jar fabric-server.jar
```

---

## 🛠️ Commands

`lucy` provides commands for managing server packages. All examples are subject to change during development.

### `search` - Find packages

Search across supported sources with filtering and sorting.

```bash
lucy search fabric/carpet
lucy search carpet --source modrinth --index downloads
```

- `-i`, `--index` — Sort by `relevance`, `downloads`, or `newest`
- `-c`, `--client` — Include client-only mods
- `-s`, `--source` — Restrict to a specific source (e.g., `modrinth`)
- `-l`, `--long` — Show hidden or collapsed output

### `add` - Install packages

Add mods, plugins, or server cores. `lucy` resolves dependencies and verifies platform compatibility.

```bash
lucy add fabric/fabric-api@latest
lucy add neoforge/create --force
```

<!-- TODO: Add screenshot -->

### `status` - Server overview

`lucy status` is a `neofetch` for Minecraft servers. You may show-off your elegantly configured server in a output form of good aesthetics:

- Game version
- Server core
- Modding platform
- List of mods/plugins
- Running status
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

- `--debug` — Show debug logs
- `--log-file` — Output the path to the logfile
- `--print-logs` — Print logs to console
- `--no-style` — Disable colored and styled output globally

---

## 📖 Syntax & Concepts

### Core Definitions

A **platform** modifies the Minecraft vanilla game (e.g., NeoForge, Fabric, MCDR) and serves as a common dependency for groups of packages. A **project** is a piece of software like a mod or plugin that relies on one or more platforms. A **package** is a compiled, ready-to-use instance of a project with a specific platform and version—the entity you actually install.

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
