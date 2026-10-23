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
task cipher:generate    # Generate cipher material (.cipher_key) from CF_API_KEY env var
task ci:test            # CI checks (optional cipher:generate, then check)
task ci:build           # CI release build + gzip artifacts
task e2e:status         # Probe generated sandboxes with the built CLI
task e2e:init           # Initialize modpack manifests from sandboxes
task e2e:install        # Run fresh-install scenarios against pinned fixtures
task e2e:compile        # Compile pinned repository fixtures
task e2e:compile:smoke  # Compile only the fixtures marked smoke
task e2e:filetrack      # Run hermetic file tracking scenarios
task sandboxes:list     # List sandbox definitions
task sandboxes:gen      # Materialize sandboxes into .sandboxes/
task logos:gen          # Regenerate status logos from terminal/assets/logos.yaml
task logos:check        # Fail when committed status logos differ from the manifest
```

Build uses ldflags to inject four cipher fragments (`keyA`/`keyB`/`ciphertextA`/`ciphertextB`) plus optional release identity (`releaseVersion`/`releaseCommit`). Dotenv loads `.env` and the single canonical cipher file `.cipher_key`, which holds all four fragments. See `docs/shared/cipher-key-operations.md`.

Status logos under `terminal/assets/{small,large}_{plain,colored}/` are generated; never edit them by hand. Change `terminal/assets/logos.yaml` (pinned source URL + sha256 and per-logo preprocessing), then run `task logos:gen -- -preview test_logos` and review the PNG previews.

To run the built binary against a test server directory:

```bash
./dist/lucy status
./dist/lucy create --minecraft 1.21.4 --core fabric@0.16.9
```

## Architecture

### Package Layout

```text
.
├── cmd/                    Root wiring (cmd_root.go) + thin Cobra subcommands
├── catalog/                Source-independent package identities, metadata, artifacts, and dependencies
├── server/                 Server product identities, environments, and bootstrap inputs
├── java/                   Java distribution vocabulary
├── javaruntime/            Java requirements, installed runtime observation, and selection
├── manifest/               Declared server intent: Minecraft, server product, requirements
├── lockfile/               Flattened resolved environment, artifacts, and descriptor modules
├── operation/              Command orchestration: init, add, remove, sync
├── install/                Convergence engine: lock replay, staged bootstrap, receipts, rollback
├── upstream/               Atomic capability interfaces: Searcher, Informer, etc.
│   └── providers/          Modrinth, CurseForge, Hangar, Spiget, MCDR, Fabric, Forge, NeoForge, Mojang, etc.
├── workspace/              Local runtime, wrapper, and package-file observation
│   ├── identify/           Associate local package files with upstream identities
│   └── discovery/          Unified installation discovery, ownership, and runtime selection
├── bootstrap/              Pinned server product bootstrap profiles
├── resolve/                Catalogue selection and descriptor dependency validation
├── input/                  Trust boundary for external input, package ref parsing
├── cache/                  Network cache request metadata and expiration policy
├── cas/                    Content-addressed storage for cached bytes
├── version/                Version values, dialects, constraint expressions, and parsing
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

`catalog` describes packages; `upstream` obtains package metadata and artifacts.
Keep `catalog` limited to the standard library and `version`. Shared usage alone
does not justify placing a type there. Manifest declarations, lock records,
workspace observations, runtime selection, and presentation stay with their owners.

`workspace` reads local files without upstream access. `workspace/discovery`
owns installation identity, member ownership, and runtime selection through one
directory scan; `workspace` assembles that result with wrappers, declarations,
and package files. `workspace/identify` connects unidentified package files to
upstream identities through hash lookup. Commands and operations invoke
identification explicitly; CLI output projections stay outside workspace
observation.

## Output Style Flags

The output style flags are persistent on the root command, and every subcommand inherits them. A command must never register a local copy that shadows the root flags.

- `--json` prints JSON indented with two spaces, and `--json-compact` prints it on one line. `--json-compact` wins when both are set. Only commands that render machine-readable output read them, through `cli.JSONMode`.
- `--no-color` strips ANSI foreground colors. Text attributes such as bold or faint survive; a color-off style keeps its bare attributes.
- `--no-interactive` disables progress bars, spinners, and prompt forms. A command that would ask for input fails instead, and the error names the flag that carries the same choice.
- `--no-style` is exactly `--no-color` combined with `--no-interactive`.
- `--yes` is not a global flag and not part of this family. It only answers a y/n prompt and never substitutes for the flag that replaces a selection.

A new interactive surface ships with its flag equivalent in the same change. User reports, Markdown, and progress rendering follow the same color state.

## Observability

- Keep presentation decisions at command boundaries; backend modules return results and warnings. Do not add combined log/report helpers.
- Log action boundaries, critical stages, decisions with reasons, state changes, recovery, and outcomes. Keep high-volume details at DEBUG.
- Log unrecovered failures once at the CLI boundary. Record successful state changes and recovery only after the corresponding work succeeds.

## Testing and Debugging

1. Always prefer the project's e2e testing suite. You should do regression tests after implementing a feature.
2. `go run ./e2e/sandbox` materializes real server sandboxes into `.sandboxes/` from `e2e/testdata/sandboxes/sandboxes.yaml`. There's brief explanation for each sandbox family in `e2e/testdata/sandboxes/docs/*.md`.
3. You may create temporary testing environments under project root with paths prefixed with `test_`. They are git ignored.
4. Do not write persistent tests when you are implementing a feature. Unit tests will be separate tasks.

The e2e domain suites are Go tests behind the `e2e` build tag, and the
filetrack suite is behind `integration`, so `go test ./...` stays unit-only.
Build Lucy and generate the sandboxes first, then:

```bash
go test -tags e2e -count=1 ./e2e/status    # probe .sandboxes
go test -tags e2e -count=1 ./e2e/init      # initialize modpack manifests
go test -tags e2e -count=1 ./e2e/compile   # compile e2e/testdata/repo fixtures
go test -tags e2e -count=1 ./e2e/install   # fresh and init-then-install scenarios
go test -tags integration -count=1 ./e2e/filetrack  # hermetic file tracking scenarios
```

Select cases with `-run`: `-run 'TestProbe/<sandbox id>'`,
`-run 'TestInitialize/<modpack id>'`, `-run 'TestCompile/<fixture id>'`,
`-run 'TestInstall/<scenario id>'`, and `-run 'TestInitInstall/<sandbox id>'`.
A suite
covers every case in its table and fails when its binary or a sandbox is
missing; probes that need manually supplied artifacts skip instead.
`TestInitialize` runs `lucy init` in a clone of each modpack sandbox and
checks the manifest and lock that the CLI writes. `TestInitInstall` initializes
each sandbox in the installation suite's table, then installs the generated manifest
and lock in an empty workspace and verifies the runtime against them. CI runs
the fresh install and the init-then-install chain as separate jobs so one
cannot mask the other. `-count=1`
disables the test cache, `-parallel N` bounds concurrency, and `-lucy` and
`-sandboxes` override the `dist/lucy` and `.sandboxes` defaults.
`e2e/internal/testkit` resolves those paths; `task e2e:status|init|compile|install` wrap
the same commands. The filetrack suite builds its own debug binary and needs neither
sandboxes nor a prebuilt CLI. CI selects the domains whose inputs changed from
the path filters in `.github/workflows/e2e.yml` and runs each selected domain
through the reusable runner.
`TestCompile` runs every fixture by default. Fixtures marked `smoke: true`
form the default CI layer; set `LUCY_COMPILE_LAYER=smoke` or run
`task e2e:compile:smoke` to select only those. CI runs the smoke layer as one
job and switches to the per-fixture full table when the pull request carries
the `ci:compile-full` label.

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
3. We are at pre-beta stage. Do not add any backward-compatibility measures nor bump document format versions upon change.
