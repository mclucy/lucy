# Agents

This document is for coding agents working on the Lucy codebase.

## Overview

Lucy is a Minecraft server package manager, like apt or cargo but for server mods/plugins. Written in Go (`github.com/mclucy/lucy`).

Users declare desired packages in a manifest. Lucy resolves versions and dependencies, writes a lock file, and installs them into a Minecraft server directory.

## Build and Development

Uses **Taskfile** (`task`).

```bash
task build              # Build debug binary → dist/lucy-{os}-{arch} + dist/lucy symlink
task dev                # Same as build
task run -- [args]      # go run from repo root with CLI args
task build:dev          # Same as build (host dev binary, incremental)
task build:watch        # Rebuild on Go file changes (file watcher)
task build:release      # Cross-compile all platforms (-tags release -w -s)
task test               # go test ./...
task test:race          # go test -race ./...
task check              # build + test + test:race
task clean              # Remove dist/ and release/ directories
task clean:dist         # Remove dist/ only
task clean:release      # Remove release/ only
task cipher:generate    # Generate cipher files from CF_API_KEY env var
task ci:test            # CI checks (optional cipher:generate, then check)
task ci:build           # CI release build + gzip artifacts
```

Build uses ldflags to inject four cipher fragments (`keyA`/`keyB`/`ciphertextA`/`ciphertextB`) plus optional release identity (`releaseVersion`/`releaseCommit`). Dotenv loads `.env`, `.cipher_key`, `.cipher_ciphertext`. See `docs/shared/cipher-key-operations.md`.

To run the built binary against a test server directory:

```bash
./dist/lucy status
./dist/lucy init --yes --game-version 1.21.4
```

## Architecture

### Entry Point

`main.go` (17 lines): `defer logger.DumpHistory(); cmd.Execute()`

### Package Layout

```text
.
├── cmd/                    Root wiring (cmd_root.go) + thin Cobra subcommands
├── types/                  Pure domain types
├── state/                  Two-file state model: lucy.yaml + lucy-lock.yaml
├── upstream/               Atomic capability interfaces: Searcher, Informer, etc.
│   └── providers/          Modrinth, CurseForge, GitHub, MCDR, Hangar, Spiget, Fabric, etc.
├── workspace/              Runtime detection — server platform, version, installed mods
│   └── detector/           Declarative runtime node detection
├── install/                RecursivePhase pipeline: Candidate→Downloaded→Verified→Committed
├── input/                  Trust boundary for external input, package ref parsing
├── cache/                  Three-layer network cache: store, index, policy
├── version/                Version parsing: semver, Minecraft release/snapshot, Maven ranges
├── artifact/               Jar metadata analysis: fabric.mod.json, mods.toml, plugin.yml
├── logger/                 Three-tier logging + Fatal, history buffer for DumpHistory()
├── tui/                    Terminal UI via bubbletea v2, lipgloss v2, huh v2, glamour
├── github/                 Shared GitHub API infra
└── internal/
    ├── cli/                Shared CLI plumbing: flags, error-logging wrapper,
    │   │                   dependency graph/data source, shell completion,
    │   │                   lock-state builders
    │   └── <command>/      Large subcommands (add, bisect, info, init, install,
    │                       search, status), each exposing NewCommand()
    ├── cipher/             ChaCha20Poly1305 encryption for API key embedding
    ├── fileschema/         Minecraft format definitions: fabric.mod.json, mods.toml, etc.
    ├── fn/                 Generic helpers: Ternary, Memoize, slice utilities
    ├── fsutil/             Filesystem helpers: CloseReader, path utilities
    ├── algo/               Graph operations, data structure utilities
    ├── slugmap/            Remote-to-local slug mapping
    └── networkutil/        Network helpers wrapping cache downloads
```

### Design Rules

- **types/ has zero dependencies.** Never import anything into types. All other packages depend on types, not the reverse.
- **Dependency inversion via atomic interfaces.** Upstream sources implement atomic capabilities (`Searcher`, `Informer`, `ArtifactMapper`, `VersionSelectorResolver`); composite interfaces (`PackageResolver`, `PackageSource`) emerge from consumers via type assertion. The install package consumes through interfaces, not concrete types.
- **State ownership is strict.** Only `ProjectStateService` reads/writes state files. Don't bypass it.
- **Three-tier logger, never `fmt.Println` for user output.** Use the logger package: file-only, user-display, or both tiers.
- **CLI layout: thin commands in `cmd/`, large commands under `internal/cli/<name>`.** A command earning its own package exposes `NewCommand() *cobra.Command` and is wired in `cmd/cmd_root.go`; shared plumbing (flags, logging wrapper, graph loading, completion, lock-state builders) lives in the `internal/cli` root package so both sides import it without import cycles.

### State Files (in user's Minecraft server directory)

- `lucy.yaml` — declared package intent (what the user wants) + optional config overrides
- `lucy-lock.yaml` — resolved dependency graph (exact versions, checksums, install paths)
- Global config: `os.UserConfigDir()/lucy/config.yaml` (user preferences, defaults)

## Researching and Designing

1. If your task is not general, i.e., the ones applicable and universal to almost any program, you should consider doing some research to know about the specific context.
2. Always do research on complicated and large features or refactors.
3. While researching, you should take reference to other package managers, such as Cargo, npm, pip, apt, brew, etc. This does not mean you should copy their design. Combine your research with our own design principles.
4. If the task is highly Minecraft-related, it is very likely that you don't have the most-updated or correct knowledge about it. Either do some research or ask me if you are not sure about something.
5. Whenever you are adding new types/enums/structs, you must elaborate and justify your design.
6. I am open to adding new packages if you think they will greatly simplify the code. Ask me before doing that.
7. You must always justify your design. Elaborate your architecture's shape and why is it.

## Tests

1. Do not add tests for new features/refactors/bug fixes unless explicitly asked.
2. Testing would be isolated tasks.
3. You are always allowed to use `go test`.

## Debugging

Generated sandbox server environments live in `.sandboxes/` (gitignored). They are materialized from the declarative manifest in `testdata/environments/environments.yaml` — run `task envs:list` to see them and `task envs:gen` to generate. Ecosystem knowledge per core family (jar formats, detector markers, download APIs) is in `testdata/environments/families/*.md`. See `docs/shared/sandbox-environments.md` for the full guide. You are allowed to create temporary sandboxes prefixed with `test_` under the project root, they are already git ignored.

### envgen CLI

`go run ./tools/envgen` materializes environments; `task envs:*` wraps it for the common cases. Flags:

| Flag | Default | Effect |
|---|---|---|
| `--list` | off | Print all environments with family, game version, generated/missing state, then exit |
| `--only a,b` | all | Restrict processing to the named environment ids |
| `--force` | off | Regenerate even when `.sandboxes/<id>/` already exists |
| `--out <dir>` | `.sandboxes` | Output root |
| `--manifest <file>` | `testdata/environments/environments.yaml` | Manifest path |
| `--cache <dir>` | `os.UserCacheDir()/lucy-envgen` | Content-addressed download cache (keyed by sha256) |
| `--manual-dir <dir>` | `<cache>/manual` | Where `manual: true` artifacts are expected as `<id>/<basename>` |

Behavior: idempotent — existing environment dirs are skipped without `--force`; every artifact digest is verified after fetch and generation aborts that environment on mismatch; missing manual artifacts fail with the exact drop-in path and expected sha256.

## Common Erros

- **Don't import into types/.** It has zero dependencies by design. If you need a type that depends on something external, it belongs in the consuming package, not types.
- **Don't use fmt.Println for user output.** The logger has three tiers for a reason. Use them.
- **Minecraft knowledge is unreliable.** Don't assume you know how mod loaders, plugin systems, or server internals work. Research or ask.
- **Package identifiers are `[source]:[platform/]name[@version]`.** Platform and version are optional. Lucy infers platform from the server environment.

## Other Rules

1. If you suspect there might be helpful packages to add, you should search on the web, or look up on go.dev.
2. If you believe the initial demand is fully satisfied and all current context will not be helpful for future tasks, you can remind me to open a new session.
