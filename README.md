<div align="center">
  <img src="images/banner.png" alt="lucy banner" width="80%" />

# Lucy

**The package manager for Minecraft servers.**

[![CI](https://github.com/mclucy/lucy/actions/workflows/ci.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/ci.yml)
[![Coverage](https://github.com/mclucy/lucy/wiki/badge/coverage.svg)](https://raw.githack.com/wiki/mclucy/lucy/dev/coverage.html)
[![License](https://img.shields.io/github/license/mclucy/lucy)](LICENSE)

[Documentation](https://lucy.lc/docs) | [English](README.md) | [中文](README_CN.md)
</div>

> [!IMPORTANT]
> Lucy is in active pre-beta development. APIs and schemas may change before 1.0. Join the [QQ Group](https://qm.qq.com/q/Sf65NVYaAi) or open an [issue](https://github.com/mclucy/lucy/issues) to contribute.

Lucy brings modern package management experience to Minecraft servers. Define and manage your server with simple and declarative workflows.

We (are working to) support multiple ecosystems, and their combinations:

- Fabric
- Forge
- NeoForge
- MCDReforged
- Bukkit/Spigot/Paper

Lucy fetches metadata and artifacts from multiple soucres:

- Modrinth
- Curseforge
- Host your own on GitHub!

## Installation

```bash
# Homebrew (macOS / Linux)
brew install --HEAD mclucy/tap/lucy

# Go
go install github.com/mclucy/lucy@latest
```

Pre-built binaries are available on [Releases](https://github.com/mclucy/lucy/releases).

## Quickstart

```bash
# Initialize in a server directory
lucy init

# Add packages with dependency resolution
lucy add fabric/fabric-api lithium sodium

# Inspect runtime platform and managed dependencies
lucy status

# Sync files deterministically from lockfile
lucy install
```

## Commands

| Command | Usage |
| --- | --- |
| `lucy init` | Scaffold `lucy.yaml` and `lucy-lock.yaml` |
| `lucy add <pkg>` | Resolve dependencies, update lockfile, and install |
| `lucy remove <pkg>` | Remove packages and prune unused transitive dependencies |
| `lucy install` | Sync server runtime deterministically from `lucy-lock.yaml` |
| `lucy status` | Show server core, loader, runtime status, and packages |
| `lucy tree` | Inspect dependency graph (supports `--live` probe) |
| `lucy leaves` | List unreferenced packages safe to remove |
| `lucy bisect` | Binary search installed mods to isolate crash causes |
| `lucy search <query>` | Query Modrinth, CurseForge, and GitHub |

Full guides, configuration, and flags: **[lucy.lc/docs](https://lucy.lc/docs)**

---

> [!NOTE]
> The logo and axolotl pixel art are copyright Mojang AB. Original replacements are in progress.
