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
task e2e:status         # Probe generated sandboxes with the built CLI
task e2e:install        # Run fresh-install scenarios against pinned fixtures
task e2e:compile        # Compile pinned repository fixtures
task sandboxes:list     # List sandbox definitions
task sandboxes:gen      # Materialize sandboxes into .sandboxes/
```

Build uses ldflags to inject four cipher fragments (`keyA`/`keyB`/`ciphertextA`/`ciphertextB`) plus optional release identity (`releaseVersion`/`releaseCommit`). Dotenv loads `.env`, `.cipher_key`, `.cipher_ciphertext`. See `docs/shared/cipher-key-operations.md`.

To run the built binary against a test server directory:

```bash
./dist/lucy status
./dist/lucy init --allow-empty --minecraft 1.21.4 --core fabric@0.16.9
```

## Architecture

### Package Layout

```text
.
├── cmd/                    Root wiring (cmd_root.go) + thin Cobra subcommands
├── types/                  Pure domain types: ecosystems, products, dependencies, artifacts
├── manifest/               Declared server intent: Minecraft, server product, requirements
├── lockfile/               Flattened resolved environment, artifacts, and descriptor modules
├── operation/              Command orchestration: init, add, remove, sync
├── install/                Convergence engine: lock replay, staged bootstrap, receipts, rollback
├── upstream/               Atomic capability interfaces: Searcher, Informer, etc.
│   └── providers/          Modrinth, CurseForge, Hangar, Spiget, MCDR, Fabric, Forge, NeoForge, Mojang, etc.
├── workspace/              Live runtime observation — server product, version, installed packages
│   └── detector/           Declarative runtime node detection
├── bootstrap/              Pinned server product bootstrap profiles
├── resolve/                Catalogue selection and descriptor dependency validation
├── input/                  Trust boundary for external input, package ref parsing
├── cache/                  Network cache request metadata and expiration policy
├── cas/                    Content-addressed storage for cached bytes
├── version/                Version parsing: semver, Minecraft release/snapshot, Maven ranges
├── artifact/               Archive metadata analysis: fabric.mod.json, mods.toml, plugin.yml
├── log/                    Operational diagnostics
├── report/                 User-facing messages
├── terminal/               Terminal rendering via the Charm stack
├── github/                 Shared GitHub API infra
└── internal/
    ├── buildrepo/           Gradle build and project discovery for compile
    ├── cli/                Shared CLI plumbing: flags,
    │   │                   dependency graph/data source, shell completion,
    │   │                   lock-context shorthand
    │   └── <command>/      Large subcommands (compile, info, search), each exposing NewCommand()
    ├── cipher/             ChaCha20Poly1305 encryption for API key embedding
    ├── fileschema/         Minecraft format definitions: fabric.mod.json, mods.toml, etc.
    ├── fn/                 Generic helpers: Ternary, slice utilities
    ├── fsutil/             Filesystem helpers: CloseReader, path utilities
    └── toolchain/          Local Git, Java, and Gradle discovery
```

## Output Style Flags

The output style flags are persistent on the root command, and every subcommand inherits them. A command must never register a local copy that shadows the root flags.

- `--json` prints JSON indented with two spaces, and `--json-compact` prints it on one line. `--json-compact` wins when both are set. Only commands that render machine-readable output read them, through `cli.JSONMode`.
- `--no-color` strips ANSI foreground colors. Text attributes such as bold or faint survive; a color-off style keeps its bare attributes.
- `--no-interactive` disables progress bars, spinners, and prompt forms. A command that would ask for input fails instead, and the error names the flag that carries the same choice.
- `--no-style` is exactly `--no-color` combined with `--no-interactive`.
- `--yes` is not a global flag and not part of this family. It only answers a y/n prompt and never substitutes for the flag that replaces a selection.

A new interactive surface ships with its flag equivalent in the same change. User reports, markdown, and progress rendering follow the same color state.

## Observability

- Keep presentation decisions at command boundaries; backend modules return results and warnings. Do not add combined log/report helpers.
- Log action boundaries, critical stages, decisions with reasons, state changes, recovery, and outcomes. Keep high-volume details at DEBUG.
- Log unrecovered failures once at the CLI boundary. Record successful state changes and recovery only after the corresponding work succeeds.

## Testing and Debugging

1. Always prefer the project's e2e testing suite. You should do regression tests after implementing a feature.
2. `go run ./e2e/sandbox` materializes real server sandboxes into `.sandboxes/` from `e2e/testdata/sandboxes/sandboxes.yaml`. There's brief explanation for each sandbox family in `e2e/testdata/sandboxes/docs/*.md`.
3. You may create temporary testing environments under project root with paths prefixed with `test_`. They are git ignored.
4. Do not write persistent tests when you are implementing a feature. Unit tests will be separate tasks.

The domain suites run with `task e2e:status -- <sandbox ids>` and
`task e2e:install -- [scenario ids]`, or directly with
`go run ./e2e/status <sandbox ids>` and `go run ./e2e/install [scenario ids]`.
`task e2e:compile -- [fixture ids]` runs the compile fixtures. Shared selection
and reporting lives in `e2e/internal/testkit`; the install suite defaults to
all scenarios, and the compile suite defaults to all repository fixtures.

### Sandbox CLI

`go run ./e2e/sandbox` materializes sandboxes; `task sandboxes:*` wraps it for the common cases. Flags:

| Flag                 | Default                                 | Effect                                                                            |
| -------------------- | --------------------------------------- | --------------------------------------------------------------------------------- |
| `--list`             | off                                     | Print all sandboxes with family, game version, generated/missing state, then exit |
| `--only a,b`         | all                                     | Restrict processing to the named sandbox ids                                      |
| `--force`            | off                                     | Regenerate even when `.sandboxes/<id>/` already exists                            |
| `--out <dir>`        | `.sandboxes`                            | Output root                                                                       |
| `--manifest <file>`  | `e2e/testdata/sandboxes/sandboxes.yaml` | Manifest path                                                                     |
| `--cache <dir>`      | `os.UserCacheDir()/lucy-sandbox`        | Content-addressed download cache (keyed by sha256)                                |
| `--manual-dir <dir>` | `<cache>/manual`                        | Where `manual: true` artifacts are expected as `<id>/<basename>`                  |

The CLI is idempotent:

- Existing sandbox dirs are skipped without `--force`
- Every artifact digest is verified after fetch and generation aborts that sandbox on mismatch
- Missing manual artifacts fail with the exact drop-in path and expected sha256.
- No binary is committed to the repository; fixtures that are not real servers pin their digests in `e2e/testdata/sandboxes/sandboxes.yaml` instead.

## Naming and Terminology

Clarify before naming. A name is a contract, not a label.

- Use one word for one concept. Do not overload a word across layers, lifecycles, or persistence boundaries.
- Reuse roots. Do not introduce a synonym when a prefix or suffix makes the meaning unambiguous.
- Name the specific layer or role. Leave the generic word unqualified only when the context makes it exact.
- Prefer terms established in software engineering, computer science, package management, or Minecraft. Avoid invented or obscure words unless they are defined at the point of use.
- Keep presentation names out of domain types and persisted schemas.

## Code Styles

- Do not write long composite literals in one line.
- Use `gofumpt`.

## Documentation

The following rules applies to README and `docs/`:

- They are user-oriented. Never edit them if your change is not worth notifying for the user.
- Never name a go module, internal terminology, or code symbol.
- The scope is strictly what the user sees. No mechanism behind it should be exposed.

## Comments

Write code that explains itself. Namings must not need documentations from call sites.

- Do not restate the code. Refactor instead if you must do this.
- No repeating comments inside or outside the package. Explain 1 fact only once across the codebase.
- Preconditions, postconditions, edge cases, why the item exists are worth recording.
- Docs must reflect only the live condition. Never record the history. That's what `git log` and `git blame` are for.
- Follow the ASD-STE100 standard.

## Other Rules

1. Always tidy the files in packages touched after refactoring: relocate/rename/merge/split files for better maintainability.
2. Prefix files with the package name for convenient searching.
