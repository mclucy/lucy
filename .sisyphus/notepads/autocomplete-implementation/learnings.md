# Learnings

## Inherited from cli-standardization-pre-autocomplete

### urfave/cli v3 Completion Behavior
- `EnableShellCompletion: true` injects hidden `completion` subcommand → `lucy completion bash|zsh|fish|pwsh`
- `--generate-shell-completion` is the framework internal handshake flag (not user-facing)
- Default `ShellComplete` only completes command names and flag names, NOT flag values or arg values
- `ShellComplete` signature: `func(context.Context, *cli.Command)` — output with `fmt.Println()` one candidate per line
- For zsh/fish descriptions: `value:description` format, framework doesn't auto-handle

### Offline Candidate Data Sources
- Static enums: `types/source.go` (4 sources), `types/type_id.go` (5 platforms), `types/search.go` (3 sort values) — usable directly
- `cache.Network().All()` returns `[]*CacheEntry` but Key=URL, Filename=artifact (e.g. `.jar`) — NOT package identifier format
- `probe.ServerInfo().Packages` gets installed packages but involves filesystem scan — performance needs evaluation
- Phase 1: mainly static enums + platform prefix; Phase 2: persistent completion index

### Key Technical Constraints
- `syntax.Parse()` will panic on syntax errors — completion MUST use lightweight prefix parsing, NOT `syntax.Parse()`
- Completion code path MUST NOT call `routing.SearchMany`/`FirstInfo` or any network paths
- `cmd/cmd_dl.go` MUST NOT be modified

### Codebase Conventions
- Flag constant naming: `flag` + CamelCase + `Name` (e.g. `flagIndexName`, `flagSourceName`)
- Completion enabled: `cmd/cmd.go` has `EnableShellCompletion: true`
- All flag lookups use constants (zero literal strings remain)
- Good reference for decorator usage: see cmd_info.go (has full decorator stack)

### Build Verification
- Build command: `go build -o /tmp/lucy-ac . && echo BUILD_OK`
- Completion test: `/tmp/lucy-ac --generate-shell-completion`
- Four shell: `/tmp/lucy-ac completion bash|zsh|fish|pwsh`
- Pre-existing vet warnings: `install/install_fabric.go:41` and `install/install_forge.go:50` (unreachable code, NOT in cmd/) — not regressions

## [2026-03-08] Task 1: Baseline Snapshot

### Build Status
- Binary builds successfully: `go build -o /tmp/lucy-ac . && echo BUILD_OK` ✓
- No build errors or regressions detected

### Shell Completion Generation
Four shell completion scripts generated via urfave/cli v3 default behavior:
- bash:  34 lines
- zsh:   29 lines
- fish:  34 lines
- pwsh:   9 lines
- Total: 106 lines

All are non-empty, confirming `EnableShellCompletion: true` is functional.

### Offline Candidate Baseline (--generate-shell-completion)
Output contains 7 default candidates (command names only):
- status
- info
- search
- add
- init
- cache
- help

Required candidates for future completion work (all present):
✓ search (keyword-based package discovery)
✓ info (detailed package information)
✓ add (package installation)
✓ cache (offline download management)

Format is `<command>:<description>` — one per line. This confirms the framework handshake is working and baseline offline completion behavior is established before any ShellComplete implementations.

### Key Observations
1. Default candidates are ONLY command names — no flag values, no package identifiers, no platform names yet
2. Shell scripts use urfave/cli v3 standard templates — descriptions for zsh/fish are framework-managed
3. No custom ShellComplete functions exist yet (confirmed by grep in plan baseline)
4. This is the true offline guardrail state before Phase 2 enrichment

### Evidence Captured
- `.sisyphus/evidence/task-1-autocomplete-baseline.txt` — shell script line counts + baseline summary
- `.sisyphus/evidence/task-1-autocomplete-baseline-error.txt` — --generate-shell-completion output + analysis

### Next Phase Ready
All baseline verification complete. Ready for Phase 2 implementation of ShellComplete functions for:
1. `search` command flags: --source, --type (static enums)
2. Positional arguments: keywords (free-form, no offline completions)
3. `add` command: platform + project + version completion (index-based in Phase 2)

## [2026-03-08] Task 2: Completion Helper Primitives

### Created: cmd/cmd_completion_helpers.go
Shared infrastructure for all shell completion functions. No network calls, no syntax.Parse().

### Exported API
- `CompletionCandidate{Value, Description}` — data struct for candidates
- `PrintCandidates()` — outputs in urfave/cli v3 `value:description` format
- `FilterByPrefix()` — case-insensitive prefix filtering
- `StaticPlatformCandidates()` — minecraft, fabric, forge, neoforge, mcdr
- `StaticSourceCandidates()` — curseforge, modrinth, github, mcdr
- `StaticSortCandidates()` — relevance, downloads, newest
- `ParseCompletionToken()` — manual parser for partial `platform/name@version` input

### Key Design Decisions
1. **Manual parsing in ParseCompletionToken**: Uses `strings.Index`/slicing, NOT `syntax.Parse()` which panics on partial input. Segments: "platform" (no `/`), "name" (`/` but no `@`), "version" (has `@`).
2. **Static candidates exclude sentinel values**: PlatformAny/None/Unknown and SourceAuto/Unknown are omitted — only user-facing values.
3. **SearchSortName excluded**: `types.SearchSort` defines `name` sort but `Valid()` returns false for it, so it's not in candidates.
4. **Description format**: Uses Title-case descriptions matching types `.Title()` methods for consistency.

### Verification
- Build: passes (BUILD_OK)
- Edge cases: `info --generate-shell-completion` and `add fabric/ --generate-shell-completion` both exit 0 with no panic
- LSP diagnostics: clean

## [2026-03-08] Task 3: search ShellComplete

### Key Finding: urfave/cli v3 ShellComplete Behavior
When `ShellComplete` is invoked during completion, the `cmd.Args().Slice()` is EMPTY because urfave/cli has already parsed the flags and consumed them. This is different from expected.

### Solution: Use os.Args Directly
Instead of using the parsed arguments, we must read `os.Args` which contains the raw command line as passed by the shell completion framework. The pattern is:
1. Read os.Args
2. Find the last argument that is NOT `--generate-shell-completion`
3. Check if that arg is a flag (e.g., `--index`, `-i`, `--source`, `-s`)
4. If yes, complete the flag's value
5. If no, complete positional arguments

### Implementation Details
- Flag values are completed by checking the last argument in os.Args
- For `--index` / `-i`: return sort candidates (relevance, downloads, newest)
- For `--source` / `-s`: return source candidates (curseforge, modrinth, github, mcdr)
- For positional args: return platform candidates, filtered by prefix

### Testing Results
All flag value completions work correctly:
- `search --index --generate-shell-completion` → sort candidates ✓
- `search --source --generate-shell-completion` → source candidates ✓
- `search fabric --generate-shell-completion` → filtered platforms ✓
- No panics on edge cases ✓

### Design Pattern
ShellComplete functions in offline completion mode:
1. Check which flag is being completed via os.Args
2. Use static data (no network calls)
3. Filter by prefix for positional args
4. Output to stdout in "value:description" format

This design is consistent with Task 2 helpers and works seamlessly with urfave/cli's completion framework.

## [2026-03-08] Task 4: info/add ShellComplete

### Source flag completion detection
- When user types `info --source <TAB>`, the shell sends `info --source --generate-shell-completion`
- `checkShellCompleteFlag` strips `--generate-shell-completion` BEFORE flag parsing → args become `info --source`
- The `--source` flag (StringFlag) with no value gets parsed as `cmd.String("source") == ""`
- Using `cmd.String(flagSourceName)` to detect the sentinel `"--generate-shell-completion"` does NOT work
- **Correct approach**: inspect `os.Args` directly — it still contains the sentinel; check `os.Args[len-2]` for `--source` or `-s`
- This mirrors what `DefaultCompleteWithFlags` does internally for the root command

### cmd.Args().Slice() in ShellComplete
- Returns only POSITIONAL (non-flag) args after the sentinel is stripped
- e.g. `add fabric/ --generate-shell-completion` → `cmd.Args().Slice() == ["fabric/"]` ✓
- e.g. `info --source --generate-shell-completion` → `cmd.Args().Slice() == []` (--source consumed as flag)

### Segment detection
- `ParseCompletionToken("fabric/")` → segment `"name"` → no offline candidates, return empty, exit 0
- `ParseCompletionToken("fabric/api@")` → segment `"version"` → no offline candidates, exit 0
- `ParseCompletionToken("fab")` → segment `"platform"`, filtered by prefix → returns `fabric:`

### Import requirement
- `"os"` needed in `cmd_info.go` for `os.Args` access; `cmd_add.go` only uses `cmd.NArg()` so no new import needed

## [2026-03-08] Task 5: Offline Candidate Aggregator

### Cache-derived candidate behavior
- `cache.Network().All()` returns `[]*cache.CacheEntry`; `Key` is canonical URL while `Filename` is the artifact name and best source for offline hints.
- Filename heuristics are safer than parsing cache keys: strip extension, split by `-`, stop at first version-like segment (`v` prefix + digits, leading digit, or dotted token).
- Cache calls are local-only but still wrapped with `recover` to keep completion path panic-safe.

### Probe-derived candidate behavior
- `probe.ServerInfo()` returns `types.ServerInfo` (no error return), so failure handling must rely on panic recovery and empty-slice fallback.
- Installed packages come from `ServerInfo.Packages`; candidate value is normalized to `platform/name` when platform is known, otherwise just name.
- Probe path can scan filesystem; aggregator keeps it optional/fail-safe by returning empty on panic or no packages.

### Aggregation design decisions
- New pipeline in `cmd/cmd_completion_candidates.go`: static platforms + cache-derived hints + local installed packages.
- De-duplication is exact by `CompletionCandidate.Value`, preserving first-seen order so static canonical values win.
- All exported functions are offline-safe and return empty slices instead of surfacing errors.

## [2026-03-08] Task 6: Unified Candidate Integration

### Changes
- Replaced `StaticPlatformCandidates()` with `AggregatePackageCandidates()` in all three command ShellComplete functions (search, info, add).
- Added consistent `maxCandidates = 50` truncation after prefix filtering to avoid overwhelming shell completions.

### Pattern consistency
- search: preserves existing `os.Args` loop for flag detection (`--index`, `--source`), only positional completion path changed.
- info: preserves `ParseCompletionToken` segment detection and `os.Args` source flag detection; only the `platform`/empty segment path changed.
- add: preserves `ParseCompletionToken` segment detection; only the `platform`/empty segment path changed.

### maxCandidates truncation
- Set to 50 as a UX safety measure. In clean environments with only 5 static platforms, truncation never activates.
- When cache + local packages grow large, truncation prevents shell from rendering hundreds of candidates.
- Truncation happens AFTER `FilterByPrefix`, so prefix-filtered results are already narrowed before truncation applies.

### Verification
- All 3 commands produce aggregated candidates (currently 5 static platforms; cache/local would add more in real environments).
- Prefix filtering confirmed case-insensitive: `FAB` → `fabric`, `neof` → `neoforge`, `for` → `forge`.
- No-match prefix (`zzz`) correctly returns empty output.
- Flag value completions (`--index`, `--source`) remain fully unchanged.

## [2026-03-08] Task 7: Performance + Side-Effect Guards

### Completion fast-path caching
- Added `sync.Once` guarded aggregation cache in `cmd/cmd_completion_candidates.go` so repeated completion calls in the same process do not rescan cache/filesystem sources.
- `AggregatePackageCandidates()` now returns a copy of cached candidates to avoid accidental shared-slice mutation by callers.
- Added panic recovery inside the `sync.Once` closure so a transient panic cannot crash completion; fallback remains empty slice.

### Safety audit findings
- `cmd/cmd_completion*.go` contains no `http.` references and no `routing.*`/`FirstInfo`/`SearchMany` references.
- `probe.ServerInfo()` is still protected by `defer recover` in `LocalInstalledCandidates()`, so completion path remains panic-safe.
- `maxCandidates = 50` truncation remains unchanged in `search`, `info`, and `add` ShellComplete paths.

### Stability verification
- Built binary with `go build -o /tmp/lucy-ac . && echo BUILD_OK`.
- Ran 5 consecutive `add --generate-shell-completion` executions; all exit codes are `0` and output hashes are identical across all runs.
- Evidence recorded in:
  - `.sisyphus/evidence/task-7-performance-guard.txt`
  - `.sisyphus/evidence/task-7-performance-guard-error.txt`

## [2026-03-08] Task 8: Multi-Shell Compatibility & Command Coverage Matrix

### Shell script generation
- All 4 shells (bash, zsh, fish, pwsh) generate non-empty completion scripts via `completion <shell>`.
- **Important**: The shell name is `pwsh`, NOT `powershell`. Using `powershell` produces an error.
- Line counts: bash=34, zsh=29, fish=34, pwsh=9.

### value:description format compatibility
- Custom ShellComplete functions output `value:description` format (e.g., `minecraft:Vanilla Minecraft`).
- zsh script uses `_describe 'values' opts` which natively parses this format.
- fish script uses `string split -m 1 ":" -- "$line"` to split value from description.
- bash uses value part only for completion matching.
- pwsh passes through to PowerShell's Register-ArgumentCompleter.
- All 4 shells handle the format correctly — no special per-shell logic needed in our code.

### Default vs custom ShellComplete behavior
- Commands WITHOUT custom ShellComplete (`cache`, `status`, `init`) use urfave/cli's default behavior.
- Default behavior auto-suggests subcommands and flags.
- `cache --generate-shell-completion` outputs: `ls`, `clear`, `help` (subcommands).
- `status` and `init` output only `help` (no other subcommands defined).
- Command aliases (e.g., `list` for `ls`, `rm` for `clear`) are NOT shown as separate candidates in completion — they work when typed but the primary name is what's completed.

### Full command coverage verified
- search: platform candidates (5) + --index sort (3) + --source (4) ✓
- info: platform candidates (5) + --source (4) ✓
- add: platform candidates (5) + prefix filtering ✓
- cache: subcommand candidates (ls, clear, help) ✓
- status: default (help) ✓
- init: default (help) ✓

### Evidence
- `.sisyphus/evidence/task-8-shell-matrix.txt` — complete 4-shell + all-command matrix
- `.sisyphus/evidence/task-8-shell-matrix-error.txt` — edge cases and error verification

## [2026-03-09] Task 9: Final QA Evidence Convergence and Contract Validation

### Full QA matrix results
- Build verification remains green: `go build -o /tmp/lucy-ac . && echo BUILD_OK` returned `BUILD_OK`.
- Four-shell generation is stable and unchanged: bash=34, zsh=29, fish=34, pwsh=9.
- `search`, `info`, and `add` completion paths return expected offline candidates for platform-prefix inputs.
- Known edge-path behaviors remain correct: `fabric/`, `fabric/fabric-api@`, and `zzz` all return empty output with exit 0.
- Default completion behavior remains intact for commands without custom argument sources (`cache`, `status`, root command list).

### Contract audit outcomes
- No network-call indicators found in completion files: no `http.`, `https.`, `routing.SearchMany`, or `FirstInfo` matches.
- `syntax.Parse` appears only in a safety comment documenting why parsing is not used in completion paths.
- `maxCandidates = 50` guard is still present in `cmd/cmd_search.go`, `cmd/cmd_info.go`, and `cmd/cmd_add.go`.

### Evidence artifacts
- `.sisyphus/evidence/task-9-final-qa.txt` — complete command matrix output.
- `.sisyphus/evidence/task-9-final-qa-error.txt` — guardrail grep/audit output.
