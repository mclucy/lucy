# e2e

End-to-end testing suite and server sandbox infrastructure for Lucy.

## Overview

The e2e domain suites are Go tests behind the `e2e` build tag. They run the built Lucy CLI (`dist/lucy`):

- `e2e/init` initializes modpack manifests from generated server sandboxes.
- `e2e/compile` compiles repository fixtures from `e2e/testdata/repo`.
- `e2e/mutation` runs heuristic add, remove, and upgrade-all cases from YAML fixtures. Its observation cases are recorded, not gated. Four contract anchors in the same package gate fixed rejection behaviors.

The `cmd` filetrack guard suite runs in the default test suite (`go test ./...`); it invokes the CLI in process and uses a helper process for the world session lock. The default suite stays hermetic through temporary directories, local HTTP servers, and self-executed helper processes.

## Running Domain Suites

Build Lucy first. Generate sandboxes only for `e2e/init`, which clones them; `e2e/compile` and `e2e/mutation` do not read `.sandboxes`:

```bash
task build                                # Build dist/lucy binary
task sandboxes:gen                        # Materialize sandboxes into .sandboxes/ (for e2e/init)

go test -tags e2e -count=1 ./e2e/init     # Initialize modpack manifests
go test -tags e2e -count=1 ./e2e/compile  # Compile e2e/testdata/repo fixtures
go test -tags e2e -count=1 -timeout 0 ./e2e/mutation # Heuristic add, remove, upgrade-all cases
```

`task e2e:init`, `task e2e:compile`, `task e2e:compile:smoke`, `task e2e:mutation`, and `task e2e:mutation:smoke` wrap these commands.

Direct `go test` runs of `e2e/mutation` need `-timeout 0`, because Go's default test timeout would kill a long run.

### Filtering and Test Flags

- Select individual cases with `-run`:
  - `TestInitialize/<modpack id>`
  - `TestCompile/<fixture id>`
  - `TestAddHeuristic/<case id>`
  - `TestRemoveHeuristic/<case id>`
  - `TestUpgradeAllHeuristic/<case id>`
- `-count=1` disables the Go test cache.
- `-parallel N` bounds test concurrency.
- `-lucy <path>` overrides the default binary path (`dist/lucy`).
- `-sandboxes <dir>` overrides the default sandbox directory (`.sandboxes`).
- Paths are resolved via `e2e/internal/testkit`.

### Suite Behaviors

- **Init (`e2e/init`)**: Runs `lucy init` in a clone of each modpack sandbox and verifies the generated manifest and lock.
- **Compile (`e2e/compile`)**: Compiles repository fixtures from `e2e/testdata/repo`. Fixtures marked `smoke: true` form the default CI layer (`LUCY_COMPILE_LAYER=smoke` or `task e2e:compile:smoke`). The full table runs when labeled with `ci:compile-full`.
- **Mutation (`e2e/mutation`)**: Heuristic corpus described below.

## Heuristic Mutation Suite

`e2e/mutation` records how the real CLI behaves on hand-authored add, remove, and upgrade-all cases. Observation cases are not a regression gate; the four contract anchors in the same package are (see [Contract Anchors](#contract-anchors)). You author concrete examples in YAML; the runner does not generate cases, randomize inputs, or assert expected outcomes for them.

### Fixture Files and Schema

Each file in `e2e/mutation/testdata/` holds a YAML sequence of cases for one command:

- `add.yaml` → `TestAddHeuristic`
- `remove.yaml` → `TestRemoveHeuristic`
- `upgrade.yaml` → `TestUpgradeAllHeuristic`

| Field      | Required | Meaning                                                                                                                                  |
| ---------- | -------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `id`       | yes      | Unique kebab-case identifier within the file; becomes the Go subtest name.                                                               |
| `smoke`    | no       | `true` includes the case in the smoke layer.                                                                                             |
| `manifest` | yes      | Literal string written byte-for-byte as `lucy.yaml`. An empty string writes an empty file. Invalid manifest text is valid input.                                                   |
| `args`     | yes      | List of CLI arguments after the command. `add` and `remove` require at least one argument. `upgrade` may use `[]` and MUST NOT pass a positional package target. Flags such as `--policy=later` are fine. |
| `setup`    | no       | List of Lucy arguments whose first word is the command, run before the main command in the same isolated root and home. `[install]` runs `lucy install` so `upgrade` has a real lock. An empty list is a fixture error; an absent key means no setup stage. |
| `files`    | no       | Map of workspace-relative path to literal file contents. MUST NOT contain `lucy.yaml`.                                                                                                              |

Unknown fields fail the test.

Authoring rules:

- Target roughly 50 cases per file, with a small smoke subset marked `smoke: true`. These counts are guidance: the runner does not enforce them, and they change over time. Make each case meaningful and distinct. Never add near-duplicate cases that differ only by a version or a trivial value.
- Prefer realistic, valid manifest YAML. Mark deliberate invalid cases by their `id` (for example, `invalid-` prefix) so reports are easy to read.
- Choose diverse, authentic packages and versions, pinning them where sensible. Do not repeat the same handful of packages across cases.
- Realistic cases model a substantial server: roughly 8 to 20 server-side mods or plugins that match the Minecraft version and loader. Avoid client-only packages (for example sodium, jei, jade, appleskin, or minimaps), even to add volume. Do not claim compatibility you have not verified; say so in the case `id` (for example `unverified-`).
- Do not add comments to fixture YAML that only restate the content.
- Add offline options where the command supports them to limit network use.
- Use workspace-relative paths only. No absolute paths, no `..`, and no secrets or tokens.

### Running

```bash
task build                                                    # Build dist/lucy binary
task e2e:mutation                                             # All cases in all three files
task e2e:mutation -- -run 'TestAddHeuristic/<case id>'        # One case
task e2e:mutation:smoke                                       # Smoke cases only
LUCY_HEURISTIC_REPORT_DIR=<absolute dir> task e2e:mutation    # Write reports to an absolute path
LUCY_HEURISTIC_COMMAND_TIMEOUT=10m task e2e:mutation          # Per-command timeout (default 3m)
```

`task e2e:mutation` runs the full layer, and `task e2e:mutation:smoke` runs the smoke layer; each task sets `LUCY_HEURISTIC_LAYER` itself. For a direct `go test` run, `LUCY_HEURISTIC_LAYER` unset or `full` runs every case; `smoke` runs only `smoke: true` cases; any other value fails the test. Each case runs with `--no-style` in an isolated temporary root and home directory, so cases do not share state or touch the developer's configuration.

### Random Sampling

`LUCY_HEURISTIC_SAMPLE=N` runs N distinct cases chosen at random from each selected top-level operation (`TestAddHeuristic`, `TestRemoveHeuristic`, `TestUpgradeAllHeuristic`). Selection happens independently per operation, after the layer filter. Unset runs every case. The sampler only chooses among existing cases; manifests remain hand-authored in the YAML fixtures, and the runner never generates or mutates them.

- `N` MUST be a positive decimal integer. Zero, non-decimal values, and `N` greater than the number of cases available after the layer filter fail the test as harness errors, so the sample never silently shrinks.
- `LUCY_HEURISTIC_SEED` is an unsigned 64-bit decimal integer and is valid only with `LUCY_HEURISTIC_SAMPLE`; setting it without a sample count fails the test. When omitted, each operation gets its own random seed. The seed is logged with `t.Logf`, which is shown only with `go test -v` or `task`; it is always recorded in `index.json`.
- Each run's `index.json` records the seed and selected case IDs for each operation. Cases listed under `sample.not_executed` have no completed index record. That can include a case interrupted after it started, so the entry does not prove the case never ran.
- The same seed, sample count, layer, and fixture bytes reproduce the same selection. A reproduced selection does not guarantee identical outcomes: cases still resolve packages over the network, so results may change when upstream data changes.
- `-run` applies after sampling. Use a top-level selector such as `-run '^TestAddHeuristic$'` to choose one operation. A subtest selector such as `TestAddHeuristic/<case id>` filters the drawn sample, so the executed count can be lower than N, and can be zero.

```bash
task build
LUCY_HEURISTIC_SAMPLE=5 LUCY_HEURISTIC_SEED=20261106 task e2e:mutation -- -run '^TestAddHeuristic$'
```

To replay one operation, rerun with its printed or indexed seed, the same sample count, and a top-level `-run` for that operation, such as `-run '^TestAddHeuristic$'`.

### Outcomes and Reports

Reports go to `LUCY_HEURISTIC_REPORT_DIR`. When unset, they go to `test_e2e-reports/mutation` at the repository root (`test_*` is git-ignored). Each run writes to `<root>/<command>/<run-id>/`, where `<command>` is `add`, `remove`, or `upgrade`, and `<run-id>` is a UTC timestamp plus 8 random hex characters (for example `20261106T083012.123456789Z-1a2b3c4d`). A run directory holds `fixture/<command>.yaml` (the exact fixture bytes), `cases/<case-id>/`, and `index.json`. Each case directory holds `report.json`, `input/` (a copy of the workspace taken before setup), and stage output files `<stage>-stdout.txt` and `<stage>-stderr.txt`: `setup-*` when the case has `setup`, and `command-*` for the case command. `report.json` holds `command`, `id`, `smoke`, `fixture`, `definition` (manifest, args, setup, files), `invocation` (`binary`, `directory`, `timeout`), `before`, an optional `setup` stage, and `main`, the case command stage (stage name `command`). Each stage holds `name`, `argv`, `outcome` (`status` is `exited`, `signaled`, `timeout`, or `skipped`, plus `exit_code`, `detail`, and `duration_ms`), `stdout`, `stderr`, `after`, and `changes`; a skipped stage has no output, snapshot, or changes. Index entries hold `id`, `smoke`, `status`, `exit_code`, `setup_status` and `setup_exit_code` (when the case has `setup`), and `report` (the path to the case `report.json`). The resolved path is logged. Read the reports to judge the CLI's behavior.

Report root: when `LUCY_HEURISTIC_REPORT_DIR` is unset but Go's `-artifacts` flag is set, reports go under the `t.ArtifactDir` root, which takes precedence over the repository fallback.

Relative `LUCY_HEURISTIC_REPORT_DIR` values resolve against `e2e/mutation`, the directory `go test` runs in, not the repository root. Pass an absolute path. The same applies to `-lucy`.

Paths: `fixture.path` in `index.json` and `report.json` is relative to the run root. Stage output paths are relative to the case directory.

Gating is limited to harness errors and contract anchor failures, which fail the test: YAML parse errors or unknown fields, invalid or duplicate ids, unsafe or duplicate file paths, a `lucy.yaml` entry in `files`, an invalid `args` shape, an empty `setup` list or a `setup` first word starting with `-`, a bad layer value, a missing Lucy binary, workspace setup or copy failures, exec startup errors, and report write failures. A setup or main command that exits non-zero, is killed by a signal, or times out is recorded as an observation and does not fail the test. When setup fails, including by a timeout or a signal, the main command is not run and its stage is recorded as `skipped` with no output. A setup that exceeds `LUCY_HEURISTIC_COMMAND_TIMEOUT` therefore skips its command, so check the setup stage before judging a skipped result. Unexpected stdout or stderr is also an observation.

### Runtime and Network

Cases execute the real CLI and may resolve packages over the network, so runs take time and depend on network availability. Each command invocation (setup and main) gets its own timeout from `LUCY_HEURISTIC_COMMAND_TIMEOUT`: a Go duration with a unit, such as `90s`, `10m`, or `1h30m`. The default is `3m`. Blank counts as unset; bare numbers, zero, and negative values fail the test as harness errors. A timeout kills the whole process tree and is recorded as an observation (`killed after <duration>`), not a failure. `report.json` records the resolved value in `invocation.timeout`, for example `3m0s`. Do not add sandbox setup to these cases; they must not depend on `.sandboxes`.

### Contract Anchors

Four tests in `e2e/mutation` check fixed rejection contracts and gate the run. They ignore the layer and sample settings, but honor `LUCY_HEURISTIC_COMMAND_TIMEOUT`. Their results are not written to heuristic reports. Run them alone with `-run '^TestMutation'`:

- `TestMutationAddRejectsMissingManifest`
- `TestMutationRemoveRejectsMissingManifest`
- `TestMutationRemoveKeepsUnparseableManifest`
- `TestMutationUpgradeKeepsManifestWithoutLock`

An anchor fails on a timeout, a clean exit, or a nonzero exit whose output lacks the expected reason. It also fails when the manifest, lock, or workspace entries differ from what the contract requires.

### Comparing Builds

Use heuristic runs to judge whether a change behaves more correctly and more usefully than the baseline. Keep the fixtures and seed fixed, and change only the binary. Both builds run the same fixture bytes because the harness comes from the same tree.

1. Build the baseline from `origin/main` (or an explicit base commit) in a git-ignored worktree, then build the changed tree:

```bash
git fetch origin && git worktree add --detach test_baseline origin/main && (cd test_baseline && task build)
task build
```

2. If the change targets a scenario no case covers, add a realistic case to the fixture first, so both builds run it.
3. Run both builds back to back with the same layer, sample, seed, timeout, and parallelism. The example draws 10 cases per operation; to compare every case, leave `LUCY_HEURISTIC_SAMPLE` unset:

```bash
export LUCY_HEURISTIC_SAMPLE=10 LUCY_HEURISTIC_SEED=20261106
LUCY_HEURISTIC_REPORT_DIR="$PWD/test_reports/baseline" go test -tags e2e -count=1 -timeout 0 -parallel 3 -v ./e2e/mutation -args -lucy "$PWD/test_baseline/dist/lucy"
LUCY_HEURISTIC_REPORT_DIR="$PWD/test_reports/changed" go test -tags e2e -count=1 -timeout 0 -parallel 3 -v ./e2e/mutation -args -lucy "$PWD/dist/lucy"
```

4. Match entries by `id` across the two `index.json` files for each command. Read the `report.json` of every case whose `status`, `exit_code`, or setup fields differ, and of every case that is not clean.
5. Inspect every observation, not only exit codes:
- `skipped` main stage: setup did not exit zero, so the command never ran. A setup timeout does this too. Read the setup stage output.
- `timeout` (`killed after <duration>`): an unfinished run, not a result. Rerun it with a larger `LUCY_HEURISTIC_COMMAND_TIMEOUT` before you judge it.
- `signaled`: read stderr for a crash.
- Unexpected stdout or stderr.
- `changes` and `after`: compare the workspace state with what the command should do.
6. Form the expected result from the case's manifest, arguments, and the contract in the package guides and `MATH.md` before you read either build's output. Grade each build's result as correct, more correct, less correct, or unclear. For user experience, check that the exit status matches the outcome, the error names the problem and the fix, and no stray output appears.
7. Fix the code, rerun with the same seed and sample, and confirm that the changed build improved without regressions in other cases.
8. Remove the worktree with `git worktree remove test_baseline` when finished.

### Promoting Invariants

Promote a behavior into a deterministic regression test only in a separate change, and only when it holds across repeated runs and follows from a logical contract. Such tests use hermetic fixtures with no network access or upstream data. Never pin unstable observations, such as network-dependent resolution, upstream versions, timing, durations, timeouts, or absolute paths. Keep those heuristic.

When adding a sandbox-backed install scenario, add its sandbox to the matching suite's `sandboxes` list in `scripts/e2e-plan.sh`. Local fixtures do not establish CI prerequisites.

## Sandbox CLI

`go run ./e2e/sandbox` materializes server sandboxes into `.sandboxes/` from `e2e/testdata/sandboxes/sandboxes.yaml`. `task sandboxes:*` wraps common commands:

- `task sandboxes:list`
- `task sandboxes:gen -- [flags]`
- `task sandboxes:clean`

Sandbox family documentation is in `e2e/testdata/sandboxes/docs/*.md`.

### Flags

| Flag                 | Default                                 | Effect                                                                            |
| -------------------- | --------------------------------------- | --------------------------------------------------------------------------------- |
| `--list`             | off                                     | Print all sandboxes with family, game version, generated/missing state, then exit |
| `--only a,b`         | all                                     | Restrict processing to the named sandbox ids                                      |
| `--force`            | off                                     | Regenerate even when `.sandboxes/<id>/` already exists                            |
| `--out <dir>`        | `.sandboxes`                            | Output root                                                                       |
| `--manifest <file>`  | `e2e/testdata/sandboxes/sandboxes.yaml` | Manifest path                                                                     |
| `--cache <dir>`      | `os.UserCacheDir()/lucy-sandbox`        | Content-addressed download cache (keyed by sha256)                                |
| `--manual-dir <dir>` | `<cache>/manual`                        | Where `manual: true` artifacts are expected as `<id>/<basename>`                  |

### Invariants & Caching

- Existing sandbox directories are skipped unless `--force` is specified.
- Every artifact digest is verified after fetch; generation aborts for that sandbox on mismatch.
- Missing manual artifacts fail with the exact drop-in path and expected sha256.
- No binary is committed to the repository; fixtures that are not real servers pin their digests in `e2e/testdata/sandboxes/sandboxes.yaml`.
