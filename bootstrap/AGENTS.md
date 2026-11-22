# bootstrap

Prepares runnable server trees inside an installation staging directory.

## Responsibilities & Boundaries

- Prepares locked server products (`PrepareLocked`) by dispatching on the lock's `bootstrap` method: `vanilla`, `paperclip`, `fabric`, `quilt`, and `forge`. An empty or unknown method is invalid.
- Staging role: populates the server directory inside a caller-supplied staging root (`stage/server.directory`). Never writes to live workspace paths and makes no ownership or receipt publication claims; staging cleanup, verification, commit, and rollback belong to callers in `install`.
- Runtime launch contracts: provides start commands and MCDR log handlers (`LaunchFor`), declared runtime outputs (`Outputs`), and superseded core file detection (`SupersededCore`).

## Invariants & Behavior

- Input extraction: places each locked input with `InputPath` from the method. Maven libraries go to `libraries/<maven path>`, the Mojang server jar goes to `server.jar` (Paperclip reads `cache/mojang_<minecraft>.jar`), and server mappings go to `server-mappings.txt`. `PrepareLocked` receives the local file of each input by lock position; an input can also name a file embedded in a Forge installer, addressed by the installer digest. Placement paths are validated (`SafeRelativePath`). Missing locked inputs fail preparation.
- Bootstrap methods:
  - `vanilla`: copies the core artifact as `server.jar`.
  - `paperclip`: copies core JAR as `server.jar` and runs Paperclip in patch-only mode (`-Dpaperclip.patchonly=true`). Requires a working Java runtime (`ErrNoJava`).
  - `fabric`: places loader JAR into Maven layout (`libraries/`), generates launch scripts (`run.sh`, `run.bat`), launcher properties, and shim JAR (`fabric-server-launch.jar`).
  - `quilt`: places the Quilt loader JAR into Maven layout (`libraries/`), copies the pinned profile libraries, and generates the launcher JAR (`quilt-server-launch.jar`) with `QuiltServerLauncher` and an embedded `quilt-server-launch.properties` selecting `KnotServer`, plus `quilt-server-launcher.properties` with `serverJar=server.jar`. It starts with `java -jar quilt-server-launch.jar nogui`. Launcher verification rejects any JAR entry beyond the manifest and embedded properties, and duplicate entries.
  - `forge`: reads the Forge or NeoForge installer's server profile, runs its processors with the locked inputs, and verifies output hashes against expected digests using Java. Both products share the installer profile format.
- Failure handling: bootstrap method errors or process execution failures return an error without touching workspace state; any staging directory cleanup is owned by the caller.

## Relevant Tests

- Exercised via the install pipeline:
  - `go test -count=1 ./install -run TestLockedReplay`
- Quilt generation and launcher rejection:
  - `go test -count=1 ./bootstrap -run TestQuilt`
