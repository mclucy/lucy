---
name: debugging-lucy-cli
description: Use when debugging or exploring the lucy Minecraft server package manager CLI - requires valid Minecraft server directory
---

# Debugging Lucy CLI

## Overview

Lucy is a Minecraft server-side package manager CLI written in Go. Debugging requires running commands in a valid Minecraft server directory.

## Working Directory Requirement

**Always** run `lucy` commands from within a valid Minecraft server directory. The program detects server type, mods, plugins, and configuration from the current working directory.

### Finding a Valid Server Directory

1. Prioritize `test_general`, this is the sandbox server where you can freely do whatever you want.
2. Check directories matching `test_*` in the project root.
3. Use `lucy status` to verify a directory is valid:

```bash
cd /path/to/server/directory
/path/to/lucy status
```

If output shows "(No server found)", the directory is not a valid Minecraft server.

**If no valid server directory exists:** Ask the user to set up a Minecraft server directory with at least a server JAR file.

## Lucy Binary Location

The compiled binary is located at:
- `dist/lucy-darwin-arm64-dev` (macOS ARM64 dev build)

## Available Commands

| Command | Purpose |
|---------|---------|
| `lucy status` | Display server info (game version, platform, mods, plugins) |
| `lucy info <package-id>` | Show detailed info about a mod/plugin |
| `lucy search <query>` | Search for mods/plugins |
| `lucy add <package-id>` | Install a mod/plugin |
| `lucy init` | Initialize lucy in current directory |
| `lucy cache` | Manage download cache |
| `lucy download <url>` | Debug command - download a URL |

### Common Flags

| Flag | Description |
|------|-------------|
| `--json` | Output JSON instead of formatted text |
| `--long, -l` | Show expanded output |
| `--no-style` | Disable colored output |
| `--debug` | Show debug logs |
| `--print-logs` | Print logs to console |
| `--log-file, -l` | Output log file path |
| `-s, --source` | Specify source (modrinth, curseforge, mcdr) |

## Debugging with `lucy status`

The most useful command for debugging is `status`:

```bash
cd /Users/skylar/Files/Developer/lucy/test_general
/Users/skylar/Files/Developer/lucy/dist/lucy-darwin-arm64-dev status
/Users/skylar/Files/Developer/lucy/dist/lucy-darwin-arm64-dev status --json
/Users/skylar/Files/Developer/lucy/dist/lucy-darwin-arm64-dev status --long
```

This reveals:
- Game version (e.g., 1.20.5)
- Server JAR (e.g., fabric-server-mc.1.20.5-loader.0.19.0-launcher.1.1.1.jar)
- Platform (Fabric, Forge, Vanilla, etc.)
- Loader version
- Mods in mods/ directory
- MCDR plugins
- Server activity (Active/Inactive, PID if running)

## Key Source Files

| File | Purpose |
|------|---------|
| `main.go` | Entry point |
| `cmd/cmd.go` | CLI command definitions |
| `cmd/cmd_status.go` | Status command implementation |
| `probe/probe.go` | Server detection and info gathering |
| `probe/internal/detector/` | Platform-specific detection (Fabric, Forge, Vanilla, MCDR) |

## Common Debugging Tasks

### 1. Test Server Detection

```bash
cd /path/to/server
/path/to/lucy status --json
```

Check the JSON output for:
- `Runtime.GameVersion` - Minecraft version
- `Runtime.DerivedModLoader()` - Platform (Fabric, Forge, etc.)
- `Packages` - Detected mods/plugins
- `Activity.Active` - Whether server is running

### 2. Debug Mod Detection

The probe package reads mods from:
- `mods/` directory for Fabric/Forge/Neoforge
- `plugins/` directory for Bukkit
- MCDR plugin directories from config

Check file paths in status output - mods must have `.jar` extension.

### 3. Test Different Server Types

Test with different server directories:
- `test_general` - Fabric 1.20.5 with MCDR
- `test_fabric_single_121` - Fabric 1.21.x
- Other `test_*` directories

## Red Flags

- **"(No server found)"** - Not running from a valid server directory
- **Empty mods list** - Check mods/ directory contains `.jar` files
- **Missing platform** - Server JAR not recognized
- **MCDR not detected** - Check config.yml for MCDR configuration

## Quick Reference

```bash
# Basic status
cd /Users/skylar/Files/Developer/lucy/test_general
/Users/skylar/Files/Developer/lucy/dist/lucy-darwin-arm64-dev status

# JSON output for parsing
/Users/skylar/Files/Developer/lucy/dist/lucy-darwin-arm64-dev status --json

# Detailed output
/Users/skylar/Files/Developer/lucy/dist/lucy-darwin-arm64-dev status --long

# Debug mode with logs
/Users/skylar/Files/Developer/lucy/dist/lucy-darwin-arm64-dev --debug --print-logs status
```