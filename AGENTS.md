# Agents

## Overview

Lucy is a Minecraft server package manager. Its goal is to provide pm-like UX for server hosting and mod pack managing.

## Toolchain

Uses **Taskfile** (`task`).

```bash
task build              # Build the host development binary → dist/lucy-{os}-{arch} + dist/lucy symlink
task build:watch        # Rebuild on Go and docs changes (file watcher)
task check              # build + test:race + lint
task clean              # Remove dist/ and release/ directories
task lint               # golangci-lint with the package dependency rules
task run -- [args]      # go run from repo root with CLI args
task test               # go test ./...
task test:race          # go test -race ./...
```

## Architecture

Lucy coordinates Minecraft server package management across distinct lifecycle boundaries: translating declarative manifests (`lucy.yaml`) into locked resolutions (`lucy-lock.yaml`), resolving and acquiring package metadata and artifacts from upstreams, staging and converging runtime files into the server workspace, recording committed ownership in the workspace installation receipt (`.lucy/install.yaml`), and observing local workspace state.

### Navigation and Package Guides

Package-specific architecture, responsibilities, invariants, and test instructions are defined locally within packages. Before making changes in any package or subsystem:

- Read applicable package-local `AGENTS.md` instructions (including parent and nested guides, when present).
- Follow both local package invariants and root repository rules.

## The Fundamental Problem

Mostly about package resolution, identification, and acquisition. See [MATH](./MATH.md).

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
5. Never assume the current code is correct. Write unit tests based on logical contracts, not asserting on whatever the current code does.

The e2e domain suites are Go tests behind the `e2e` build tag. The `cmd` filetrack guard suite runs in the default suite; it invokes the CLI in process and uses a helper process for the world session lock. The default suite stays hermetic through temporary directories, local HTTP servers, and self-executed helper processes. Build Lucy and generate the sandboxes first, then:

```bash
go test -tags e2e -count=1 ./e2e/status    # probe .sandboxes
go test -tags e2e -count=1 ./e2e/init      # initialize modpack manifests
go test -tags e2e -count=1 ./e2e/compile   # compile e2e/testdata/repo fixtures
go test -tags e2e -count=1 ./e2e/install   # fresh and init-then-install scenarios
```

Select cases with `-run`: `-run 'TestProbe/<sandbox id>'`, `-run 'TestInitialize/<modpack id>'`, `-run 'TestCompile/<fixture id>'`, `-run 'TestInstall/<scenario id>'`, and `-run 'TestInitInstall/<sandbox id>'`. A suite covers every case in its table and fails when its binary or a sandbox is missing; probes that need manually supplied artifacts skip instead. `TestInitialize` runs `lucy init` in a clone of each modpack sandbox and checks the manifest and lock that the CLI writes. `TestInitInstall` initializes each sandbox in the installation suite's table, then installs the generated manifest and lock in an empty workspace and verifies the runtime against them. CI runs the fresh install and the init-then-install chain as separate jobs so one cannot mask the other. `-count=1` disables the test cache, `-parallel N` bounds concurrency, and `-lucy` and `-sandboxes` override the `dist/lucy` and `.sandboxes` defaults. `e2e/internal/testkit` resolves those paths; `task e2e:status|init|compile|install` wrap the same commands. The `cmd` filetrack guard suite runs the CLI in process and needs neither sandboxes nor a prebuilt binary. CI selects the domains whose inputs changed from the path filters in `.github/workflows/e2e.yml` and runs each selected domain through the reusable runner. `TestCompile` runs every fixture by default. Fixtures marked `smoke: true` form the default CI layer; set `LUCY_COMPILE_LAYER=smoke` or run `task e2e:compile:smoke` to select only those. CI runs the smoke layer as one job and switches to the per-fixture full table when the pull request carries the `ci:compile-full` label.

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

Documentation has two audiences. Choose the layer before writing:

### Human-facing documentation

- The README and published pages outside `docs/agents/` are for users. Change them when a user-facing change is worth documenting.
- Write concise, task-oriented tutorials in natural language. Preserve useful examples, prerequisites, safety warnings, and decisions; do not enumerate obvious behavior or restate command help or source code.
- Describe only what users need to see or do. Do not expose implementation mechanisms, internal terminology, Go packages, or code symbols.
- Respect existing human prose and structure. Improve unclear language without replacing it with generic or repetitive text.

### Agent-facing documentation

- Put agent pages in `docs/agents/*.mdx`. Keep them accessible by direct Mintlify URL and searchable through `lucy docs grep`, but do not add them to site navigation or other user-interface entry points.
- Explain Lucy's actual internal architecture and domain models: ownership, state, boundaries, data flow, invariants, and failure or recovery semantics. Ground claims in the current source.
- Pages in `docs/agents/` may use technical and internal terminology, but mention internal package names or Go symbols only when necessary to explain a concept or help a reader, not exhaustively; preserve their conceptual role and searchability.
- Do not duplicate human tutorials, command reference material, or agent-specific usage workflows. Agents learn how to use Lucy from the human documentation.
- Keep both layers consistent with the implementation. When agent pages change, rebuild Lucy and verify that the new pages can be found with `lucy docs grep`.
- Default builds embed the tracked `.mdx` pages, while release builds embed the archive packed by `task docs:pack`. Add a new page directory to the embed patterns in `docs/embed_pages.go`, or that directory's pages reach release binaries only.

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
