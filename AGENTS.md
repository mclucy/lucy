# Agents

This document is for coding agents working on the Lucy codebase.

## What is Lucy?

Lucy is a Minecraft server package manager, like apt or cargo but for server mods/plugins. Written in Go 1.26.4 (`github.com/mclucy/lucy`). Apache 2.0 licensed.

Users declare desired packages in a manifest. Lucy resolves versions and dependencies, writes a lock file, and installs them into a Minecraft server directory.

## Git Rules

1. If not otherwise specified, you are allowed to make commits.
2. You are always **NOT** allowed to perform `git push` or `git pull`.
3. Do not create new branches or worktrees if not explicitly asked.

## Build and Development

Uses **Taskfile** (`task`), not Make.

```bash
task build              # Clean + build debug binary → dist/lucy-{os}-{arch}-dev
task dev                # Same as build
task run -- [args]      # Build + run debug binary with CLI args
task build:dev-core     # Incremental build (no clean) for fast iteration
task build:watch        # Rebuild on Go file changes (file watcher)
task build:release      # Cross-compile all platforms (-tags release -w -s)
task test               # go test ./...
task test:race          # go test -race ./...
task check              # build:dev-core + test + test:race
task smoke              # Build + verify CLI entrypoints parse
task verify             # check + smoke (full pre-commit gate)
task clean              # Remove dist/ and release/ directories
task clean:dist         # Remove dist/ only
task clean:release      # Remove release/ only
task cipher-generate    # Generate cipher files from CF_API_KEY env var
task copyright-add      # Add Apache 2.0 license headers
task copyright-remove   # Remove copyright headers
```

Build uses ldflags to inject cipher key+ciphertext via `-X github.com/mclucy/lucy/internal/cipher.Key=$KEY`. Dotenv loads `.env`, `.cipher_key`, `.cipher_ciphertext`.

To run the built binary against a test server directory:

```bash
./dist/lucy-darwin-arm64-dev status
./dist/lucy-darwin-arm64-dev init --yes --game-version 1.21.4
```

## Architecture

### Entry Point

`main.go` (17 lines): `defer logger.DumpHistory(); cmd.Execute()`

### Package Layout

```
.
├── cmd/                    Cobra CLI commands (14+ files)
│   └── init/               Large init state machine sub-package
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

### State Files (in user's Minecraft server directory)

- `lucy.yaml` — declared package intent (what the user wants) + optional config overrides
- `lucy-lock.yaml` — resolved dependency graph (exact versions, checksums, install paths)
- Global config: `os.UserConfigDir()/lucy/config.yaml` (user preferences, defaults)

### Key Dependencies

cobra v1.10.2, bubbletea v2, huh v2, lipgloss v2, semver v3, go-toml, glamour, fuzzy, ini.v1, yaml.v3

## Researching and Designing

1. If your task is not general, i.e., the ones applicable and universal to almost any program, you should consider doing some research to know about the specific context.
2. Always do research on complicated and large features or refactors.
3. While researching, you should take reference to other package managers, such as Cargo, npm, pip, apt, brew, etc. This does not mean you should copy their design. Combine your research with our own design principles.
4. If the task is highly Minecraft-related, it is very likely that you don't have the most-updated or correct knowledge about it. Either do some research or ask me if you are not sure about something.
5. Whenever you are adding new types/enums/structs, you must elaborate and justify your design.
6. I am open to adding new packages if you think they will greatly simplify the code. Ask me before doing that.
7. You must always justify your design. Elaborate your architecture's shape and why is it.

## Testing

1. Do not add, propose, or even waste time on thinking about tests if not explicitly asked.
2. Tests will always be prompted as isolated tasks.
3. You are always allowed to use `go test` to audit.

## Debugging

You may find `test_*` directories under the project root. They are sandbox servers for smoke tests. They are .gitignored so you might not be able to find them with provided `grep` or `glob`, use `ls` to discover them instead.

## Common Gotchas

- **Don't import into types/.** It has zero dependencies by design. If you need a type that depends on something external, it belongs in the consuming package, not types.
- **Don't bypass ProjectStateService.** All state file reads/writes go through it. Direct file manipulation breaks atomicity guarantees.
- **Don't use fmt.Println for user output.** The logger has three tiers for a reason. Use them.
- **Minecraft knowledge is unreliable.** Don't assume you know how mod loaders, plugin systems, or server internals work. Research or ask.
- **Upstream providers are routed by Source enum.** `hangar` and `spiget` are defined but not wired into the resolver. Don't assume they work.
- **The cipher system embeds API keys at build time.** `task cipher-generate` requires `CF_API_KEY` in the environment. Without it, CurseForge integration won't work.
- **Package identifiers are `[source]:[platform/]name[@version]`.** Platform and version are optional. Lucy infers platform from the server environment.

## Other Rules

1. If you suspect there might be helpful packages to add, you should search on the web, or look up on go.dev.
2. If you believe the initial demand is fully satisfied and all current context will not be helpful for future tasks, you can remind me to open a new session.
