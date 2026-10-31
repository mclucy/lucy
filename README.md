<div align="center">
  <img src="images/banner.png" alt="lucy banner" width="80%" />

# Lucy

**The package manager for Minecraft servers.**

[![CI](https://github.com/mclucy/lucy/actions/workflows/ci.yml/badge.svg)](https://github.com/mclucy/lucy/actions/workflows/ci.yml)
[![Coverage](https://github.com/mclucy/lucy/wiki/badge/coverage.svg)](https://raw.githack.com/wiki/mclucy/lucy/badge/coverage.html)
[![License](https://img.shields.io/github/license/mclucy/lucy)](LICENSE)

[Documentation](https://lucy.lc/docs) | [English](README.md) | [中文](README_CN.md)
</div>

> [!IMPORTANT]
> Lucy is in active pre-beta development. APIs and schemas may change before `v0.1.0`. Join
> the [QQ Group](https://qm.qq.com/q/Sf65NVYaAi) or open an [issue](https://github.com/mclucy/lucy/issues/new) to
> contribute.

Lucy brings modern package management experience to Minecraft servers. Define and manage your server with simple and
declarative workflows.

We (are working to) support multiple ecosystems, and their combinations:

- Fabric
- Forge
- NeoForge
- MCDReforged
- Bukkit/Spigot/Paper

Lucy fetches metadata and artifacts from multiple sources:

- Modrinth
- CurseForge
- Host your own on GitHub!

## Installation

macOS and Linux:

```sh
curl -fsSL https://lucy.lc/sh | sh
```

The installer places Lucy in `~/.local/bin`; add that directory to your `PATH` if needed.

Windows (PowerShell):

```powershell
irm https://lucy.lc/ps1 | iex
```

The installer adds `%USERPROFILE%\.local\bin` to your user PATH. Open a new terminal to use it.

Other installation options:

```bash
# Homebrew (macOS / Linux)
brew tap mclucy/tap
brew trust --tap mclucy/tap
brew install --cask lucy

# Go
go install github.com/mclucy/lucy@latest

# Nix (flake)
nix run github:mclucy/lucy

# Docker (CLI only)
docker pull ghcr.io/mclucy/lucy
```

Pre-built binaries are available on [Releases](https://github.com/mclucy/lucy/releases).

The container image runs the CLI against a mounted server directory; it does not start the server. See [Containers](https://lucy.lc/docs/containers).

On NixOS or with home-manager, add the flake as an input, import `nixosModules.default` or `homeManagerModules.default`, and set `programs.lucy.enable = true;`.

## Quickstart

```bash
# Create a new server
lucy create

# Or, instead of create, adopt an existing server directory:
# lucy init

# Add packages with dependency resolution
lucy add modrinth:fabric-api modrinth:lithium

# Inspect the live server
lucy status

# Sync the server as defined by the manifest file
lucy install
```

`lucy.yaml` records your server setup. `lucy-lock.yaml` records the exact artifact that satisfies it. With the lock, lucy can recreate any server, no matter how complicated it is.

## Agent Support

We treat agents as serious as we treat our human users. Agents can use lucy efficently and smoothly. You don't even need to configure a MCP server (they only fill up your context window with tool definitions), not even skills. Install lucy, and tell your agents to use it.

Try this prompt:

```text
Use `lucy` to host a Technical Minecraft server with Minecraft 26.2 for me.
```

## Commands

| Command                | Usage                                                                                               |
| ---------------------- | --------------------------------------------------------------------------------------------------- |
| `lucy create`          | Create a new server in a directory and install it; `--no-install` writes only the manifest and lock |
| `lucy init`            | Generate `lucy.yaml` and `lucy-lock.yaml` from an existing server directory                         |
| `lucy add <pkg>...`    | Record requirements, resolve the closure, update the lock, and sync                                 |
| `lucy remove <pkg>...` | Drop requirements, re-resolve, and prune artifacts nothing needs                                    |
| `lucy deinit`          | Remove lucy's manifest, lock, and local state from a server directory                               |
| `lucy install`         | Converge the server with the lock; re-resolve when stale, with `--locked` and `--offline` controls  |
| `lucy status`          | Show the live server identity, files, and packages; `--from-lock` renders the lock                  |
| `lucy search <query>`  | Search Modrinth, CurseForge, Hangar, Spiget, and the MCDReforged catalogue                          |

Full guides, configuration, and flags: **[lucy.lc/docs](https://lucy.lc/docs)**

---

> [!NOTE]
> The logo and axolotl pixel art are copyright Mojang AB. Original replacements are in progress.
