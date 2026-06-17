---
name: debugging-lucy-cli
description: Use when debugging or exploring the lucy Minecraft server package manager CLI, command behavior, probe/status output, installer flows, cache state, logs, or local Minecraft server fixtures.
---

# Debugging Lucy CLI

## Overview

Lucy is a Go CLI for taking over and managing Minecraft server directories. Debugging is usually about one of three things: command wiring in `cmd/`, environment detection in `probe/`, or install/cache/provider behavior after a valid server directory has been selected.

**Core rule:** run `lucy` commands from the server directory you want to inspect, and use `status --json` plus logs before guessing from files alone.

## Working Directory Requirement

Most commands inspect the current working directory for server jars, mods, plugins, MCDR config, and `.lucy` state. Run the binary from a fixture or real server directory:

```bash
cd /Users/skylar/Files/Developer/lucy/test_general
/Users/skylar/Files/Developer/lucy/dist/lucy-darwin-arm64-dev status --json
```

If status reports `(No server found)` or `(Unresolved)`, first check whether the server jar is at a detectable location and whether the fixture is meant to be a jar-only edge case.

## Build and Binary

| Need | Command or path |
|---|---|
| Dev binary | `dist/lucy-darwin-arm64-dev` |
| Build once | `task build:dev` |
| Incremental rebuild | `task build:dev-core` |
| Watch rebuild | `task build:watch` |
| Run without binary | `go run . <command>` |

Dev builds use the `debug` build tag, which auto-enables debug logging via `logger/logger_tag_debug.go`.

## Current Command Surface

Registered top-level commands are defined in `cmd/root.go` plus individual `cmd/cmd_*.go` files.

| Command | Purpose | Key flags |
|---|---|---|
| `lucy status` | Show server topology, packages, and activity | `--json`, `--long/-l`, `--no-style` |
| `lucy init` | Create or reconcile `.lucy` state | `--yes/-y`, `--conflict/-c`, `--game-version`, hidden `--work-dir` |
| `lucy add <ids...>` | Add packages, resolve deps, update manifest/lock/runtime | `--force/-f`, `--with-optional`, `--no-optional` |
| `lucy remove <ids...>` | Remove requested packages and prune transitive deps | `--no-style` |
| `lucy install` | Sync lockfile state into runtime files | `--no-style` |
| `lucy search <query>` | Search upstream package sources | `--index/-i`, `--client/-c`, `--platform`, `--source/-s`, `--json`, `--long/-l` |
| `lucy info <id>` | Show upstream package metadata | `--source/-s`, `--json`, `--long/-l` |
| `lucy cache ls|clear` | Inspect or clear download cache | `--json` on `ls` |
| `lucy cache slugs ls|clear` | Inspect or clear slug-resolution cache | `--json` on `ls` |
| `lucy completion` | Cobra-generated shell completions | shell subcommands |

There is no registered `lucy download` command. `cmd/cmd_config.go` defines a stub `config` command, but it is intentionally not added to `rootCmd`.

## Global Debug Flags

| Flag | Notes |
|---|---|
| `--debug` | Enables debug logs |
| `--print-logs` | Mirrors file logs to console |
| `--dump-logs` | Hidden; dumps log history at exit through `logger.DumpHistory()` |
| `--log-file` | Prints the session log file path; no short flag |
| `--no-style` | Disables styled output |

`-l` means `--long`, not `--log-file`.

Useful logging patterns:

```bash
# Show log file path and detailed probe output
/Users/skylar/Files/Developer/lucy/dist/lucy-darwin-arm64-dev --log-file --debug --print-logs status --long

# Dump buffered history on exit for post-mortem debugging
/Users/skylar/Files/Developer/lucy/dist/lucy-darwin-arm64-dev --debug --dump-logs add modrinth:sodium
```

## Server Fixtures

Prefer fixtures that are known to be runnable for command debugging; use unresolved fixtures when debugging detector gaps.

| Fixture | Use for | Notes |
|---|---|---|
| `test_general` | Primary sandbox | NeoForge 20.2.93 / MC 1.20.2 with MCDR and `.lucy` state |
| `test_fabric_single_121` | Clean Fabric detection | Fabric 1.21.4 with a small mod set |
| `test_neoforge` | NeoForge detection/install checks | NeoForge 21.x fixture |
| `test_gtnh` | Large modpack stress | Many mods; noisy but good for scale |
| `test_mohist` | Hybrid Forge/Bukkit behavior | Mohist-style fixture |
| `test_proprietary` | Bukkit/Paper-fork plugin detection | Proprietary Paper-family fork with plugins |
| `test_paper_family/*` | Paper-family detector edge cases | Many jars are nested or jar-only; unresolved output can be expected |
| `test_sponge*`, `test_arclight_fabric`, `test_catserver` | Detector edge cases | Often jar-only or unresolved; useful for probe work, not general command debugging |
| `test_sandk` | CurseForge modpack archive inspection | Zip/overrides fixture, not a runnable server |

## Status and Probe Workflow

Start every environment bug with:

```bash
cd /Users/skylar/Files/Developer/lucy/test_general
/Users/skylar/Files/Developer/lucy/dist/lucy-darwin-arm64-dev --debug --print-logs status --json
```

Inspect these JSON areas:

- `Runtime.GameVersion` for Minecraft version
- `Runtime` / derived topology fields for platform and loader information
- `Packages` for detected mods, plugins, and managed packages
- `Activity.Active` and PID-related fields for server process detection

If changing detection code, remember `probe.ServerInfo()` is memoized. Use `probe.Rebuild()`, `probe.InvalidateServerInfo()`, `probe.ServerInfoAt(workDir)`, or `probe.RefreshServerInfo(workDir)` as appropriate instead of assuming a second call reprobes automatically.

## Key Source Files

| File | Purpose |
|---|---|
| `main.go` | Entrypoint; defers `logger.DumpHistory()` and calls `cmd.Execute()` |
| `cmd/root.go` | Cobra root command, persistent flags, `runWithErrorLogging` |
| `cmd/cmd_common_flags.go` | Shared flag names and helpers |
| `cmd/cmd_status.go` | Status command and JSON/long output behavior |
| `cmd/cmd_add.go`, `cmd/cmd_remove.go`, `cmd/cmd_install.go` | Mutating package commands |
| `cmd/command_contracts.go` | Semantic contracts and mutation boundaries for add/remove/install |
| `cmd/cmd_cache.go` | Download and slug cache commands |
| `probe/probe.go` | Memoized server info build and refresh APIs |
| `probe/internal/detector/` | Platform-specific detection |
| `install/install.go`, `install/install_many.go` | Installer entry points and recursive resolution loop |
| `logger/` | Debug, print, dump-history, and log-file behavior |

## Common Debugging Tasks

### Command wiring bug

1. Check `cmd/root.go` for persistent flags and root behavior.
2. Check the relevant `cmd/cmd_<name>.go` file for args, flags, `PreRunE`, and `RunE`.
3. Confirm the command is registered with `rootCmd.AddCommand(...)`.
4. For mutating commands, check `cmd/command_contracts.go` before changing behavior.

### Server detection bug

1. Reproduce with `status --json --long` in the smallest relevant fixture.
2. Compare a runnable fixture (`test_general`, `test_fabric_single_121`, `test_neoforge`) with an edge fixture (`test_paper_family/*`, `test_sponge*`, `test_catserver`).
3. Trace `probe.ServerInfo()` into `probe/internal/detector/`.
4. Account for probe memoization before concluding a fix failed.

### Install/add/remove bug

1. Use `test_general` unless the bug requires a specific topology.
2. Run with `--debug --print-logs`; add `--dump-logs` if the failure exits early.
3. Trace `cmd/cmd_add.go` or `cmd/cmd_remove.go` into `install.InstallMany()` and provider routing.
4. Inspect `.lucy/manifest.toml`, `.lucy/lock.json`, runtime `mods/`/`plugins/`, and cache state.

### Cache/provider bug

Use `lucy cache ls --json` and `lucy cache slugs ls --json` before clearing state. Clearing caches can hide whether a bug is stale-cache behavior or provider/download behavior.

## Red Flags

| Symptom | First check |
|---|---|
| `(No server found)` | Current working directory and root-level detectable server jar |
| `(Unresolved)` topology | Jar-only or nested fixture; try a runnable fixture for baseline |
| Empty mods/plugins | `mods/`, `plugins/`, MCDR config paths, and jar extensions |
| Command missing | Confirm `rootCmd.AddCommand(...)`; ignore README-only commands not in code |
| `-l` behaves unexpectedly | It is `--long`; use `--log-file` for log path |
| Repeated probe output unchanged | Probe cache may not have been invalidated |

## Quick Reference

```bash
# Build current debug binary
task build:dev

# Primary sandbox status
cd /Users/skylar/Files/Developer/lucy/test_general
/Users/skylar/Files/Developer/lucy/dist/lucy-darwin-arm64-dev status --json --long

# Debug logs to console
/Users/skylar/Files/Developer/lucy/dist/lucy-darwin-arm64-dev --debug --print-logs status

# Inspect caches without mutating them
/Users/skylar/Files/Developer/lucy/dist/lucy-darwin-arm64-dev cache ls --json
/Users/skylar/Files/Developer/lucy/dist/lucy-darwin-arm64-dev cache slugs ls --json
```
