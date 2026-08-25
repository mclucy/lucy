# Agents

## Overview

Lucy is a Minecraft server package manager. Its goal is to provide pm-like UX for server hosting and mod pack managing.

## Toolchain

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

## Researching and Designing

1. If your task is not general, i.e., the ones applicable and universal to almost any program, you should consider doing some research to know about the specific context.
2. Always do research on complicated and large features or refactors.
3. While researching, you should take reference to other package managers, such as Cargo, npm, pip, apt, brew, etc. This does not mean you should copy their design. Combine your research with our own design principles.
4. If the task is highly Minecraft-related, it is very likely that you don't have the most-updated or correct knowledge about it. Either do some research or ask me if you are not sure about something.
5. Whenever you are adding new types/enums/structs, you must elaborate and justify your design.
6. I am open to adding new packages if you think they will greatly simplify the code. Ask me before doing that.
7. You must always justify your design. Elaborate your architecture's shape and why is it.

## Testing and Debugging

1. Always prefer the project's e2e testing suite (`envgen`). You should do regression tests after implementing a feature.
2. `envgen` creates sandbox server environments in `.sandboxes/` with the manifest file `testdata/environments/environments.yaml`. There's brief explanation for each environment in `testdata/environments/families/*.md`.
3. You may create temporary testing environments under project root with paths prefixed with `test_`. They are git ignored.
4. Upon refactors/bug fixes/feature additions, you may write temporary go test files for PoC but you must dispose them afterwards.
5. Do not create persisted tests unless explicitly asked.

### envgen CLI

`go run ./tools/envgen` materializes environments; `task envs:*` wraps it for the common cases. Flags:

| Flag | Default | Effect |
| --- | --- | --- |
| `--list` | off | Print all environments with family, game version, generated/missing state, then exit |
| `--only a,b` | all | Restrict processing to the named environment ids |
| `--force` | off | Regenerate even when `.sandboxes/<id>/` already exists |
| `--out <dir>` | `.sandboxes` | Output root |
| `--manifest <file>` | `testdata/environments/environments.yaml` | Manifest path |
| `--cache <dir>` | `os.UserCacheDir()/lucy-envgen` | Content-addressed download cache (keyed by sha256) |
| `--manual-dir <dir>` | `<cache>/manual` | Where `manual: true` artifacts are expected as `<id>/<basename>` |

The CLI is idempotent:

- Existing environment dirs are skipped without `--force`
- Every artifact digest is verified after fetch and generation aborts that environment on mismatch
- Missing manual artifacts fail with the exact drop-in path and expected sha256.

## Other Rules

1. If you suspect there might be helpful packages to add, you should search on the web, or look up on go.dev.
2. If you believe the initial demand is fully satisfied and all current context will not be helpful for future tasks, you can remind me to open a new session.
3. Upon refactors, always reconsider the file structure of touched packages. You should do a cleanup by moving/renaming/merging/splitting files for better maintainability.
4. Prefix files with the package name for convinent searching.
