# bootstrap

Prepares runnable server trees inside an installation staging directory.

## Responsibilities & Boundaries

- Prepares locked server products (`PrepareLocked`) by dispatching to bootstrapper adapters: empty method (`""`) for plain, `paperclip`, `fabric`, and `forge-installer`.
- Staging role: populates the server directory inside a caller-supplied staging root (`stage/server.directory`). Never writes to live workspace paths and makes no ownership or receipt publication claims; staging cleanup, verification, commit, and rollback belong to callers in `install`.
- Runtime launch contracts: provides start commands and MCDR log handlers (`LaunchFor`), declared runtime outputs (`Outputs`), and superseded core file detection (`SupersededCore`).

## Invariants & Behavior

- Input extraction: extracts locked inputs and embedded archive files using validated relative destination paths (`SafeRelativePath`); source artifact paths are supplied by the caller. Missing locked inputs fail preparation.
- Execution adapters:
  - Empty (`""`): copies core artifact as `server.jar`.
  - `paperclip`: copies core JAR as `server.jar` and runs Paperclip in patch-only mode (`-Dpaperclip.patchonly=true`). Requires a working Java runtime (`ErrNoJava`).
  - `fabric`: places loader JAR into Maven layout (`libraries/`), generates launch scripts (`run.sh`, `run.bat`), launcher properties, and shim JAR (`fabric-server-launch.jar`).
  - `forge-installer`: unpacks installer data, processes profile outputs, and verifies output hashes against expected digests using Java.
- Failure handling: adapter errors or process execution failures return an error without touching workspace state; any staging directory cleanup is owned by the caller.

## Relevant Tests

- Exercised via the install pipeline:
  - `go test -count=1 ./install -run TestLockedReplay`
