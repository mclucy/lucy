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

- Always prefer the project's e2e testing suite for regression testing after implementing features. See [`e2e/AGENTS.md`](./e2e/AGENTS.md) for domain suites, sandbox generation, and runner options.
- The e2e suite includes a heuristic test set (`e2e/mutation`). Its observation cases assert no specific behavior; they record each outcome faithfully. Four contract anchors in the same package do gate fixed rejection behaviors. Use the observations to judge whether your change produces more correct and more desirable results than the baseline. See [`e2e/AGENTS.md`](./e2e/AGENTS.md#heuristic-mutation-suite) for the comparison workflow.
- Judge heuristic results against the expected behavior from the contract, not against the current output or the baseline. Compare baseline and changed builds on the same fixtures and seed, in equivalent environments. Inspect skipped stages, timeouts, and unexpected stdout or stderr, not only exit codes. Judge user experience too: error messages should name the problem and the fix.
- Promote an invariant into a deterministic regression test only in a separate change, and only after it holds across repeated runs and follows from a logical contract. Never pin unstable observations, such as network-dependent resolution, upstream versions, timing, or timeouts.
- You may create temporary testing environments under the repository root with paths prefixed with `test_` (git-ignored).
- Do not write persistent tests when you are implementing a feature (unit tests will be separate tasks).
- Never assume the current code is correct. Write unit tests based on logical contracts, not asserting on whatever the current code does.

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
