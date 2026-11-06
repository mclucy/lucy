# e2e

End-to-end testing suite and server sandbox infrastructure for Lucy.

## Overview

The e2e domain suites are Go tests behind the `e2e` build tag. They exercise real server sandboxes, modpack initialization, fixture compilation, and installation workflows against the built Lucy CLI binary (`dist/lucy`).

The `cmd` filetrack guard suite runs in the default test suite (`go test ./...`); it invokes the CLI in process and uses a helper process for the world session lock. The default suite stays hermetic through temporary directories, local HTTP servers, and self-executed helper processes.

## Running Domain Suites

Build Lucy and generate the required sandboxes first, then run the domain suites:

```bash
task build                                # Build dist/lucy binary
task sandboxes:gen                        # Materialize sandboxes into .sandboxes/

go test -tags e2e -count=1 ./e2e/status    # Probe .sandboxes
go test -tags e2e -count=1 ./e2e/init      # Initialize modpack manifests
go test -tags e2e -count=1 ./e2e/compile   # Compile e2e/testdata/repo fixtures
go test -tags e2e -count=1 ./e2e/install   # Fresh and init-then-install scenarios
```

`task e2e:status`, `task e2e:init`, `task e2e:compile`, and `task e2e:install` wrap these commands.

### Filtering and Test Flags

- Select individual cases with `-run`:
  - `TestProbe/<sandbox id>`
  - `TestInitialize/<modpack id>`
  - `TestCompile/<fixture id>`
  - `TestInstall/<scenario id>`
  - `TestInitInstall/<sandbox id>`
- `-count=1` disables the Go test cache.
- `-parallel N` bounds test concurrency.
- `-lucy <path>` overrides the default binary path (`dist/lucy`).
- `-sandboxes <dir>` overrides the default sandbox directory (`.sandboxes`).
- Paths are resolved via `e2e/internal/testkit`.

### Suite Behaviors

- **Status (`e2e/status`)**: Probes server runtime and packages in `.sandboxes`. Probes requiring manually supplied artifacts skip automatically when those artifacts are absent.
- **Init (`e2e/init`)**: Runs `lucy init` in a clone of each modpack sandbox and verifies the generated manifest and lock.
- **Compile (`e2e/compile`)**: Compiles repository fixtures from `e2e/testdata/repo`. Fixtures marked `smoke: true` form the default CI layer (`LUCY_COMPILE_LAYER=smoke` or `task e2e:compile:smoke`). The full table runs when labeled with `ci:compile-full`.
- **Install (`e2e/install`)**: Tests fresh installation and the init-then-install chain in empty workspaces, verifying runtime state against manifest and lock. CI runs fresh install and init-then-install as separate jobs.

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
