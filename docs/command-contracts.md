# Lucy command contracts

This document fixes one meaning for `lucy add`, `lucy remove`, and `lucy install`.
It keeps the operator in control and defines which layer each command owns:

- manifest = human intent
- lockfile = exact resolved facts
- runtime = files and directories in the current server
- observed state = fresh probe output used for validation and drift detection

These contracts are intentionally stricter than the current implementation. They
exist so later command work can converge without semantic overlap.

## Shared rules

- Probe output remains observed state. It informs commands, but it does not
  become manifest intent by itself.
- `ignored` entries stay unmanaged. They may be observed, but commands must not
  rewrite or delete them.
- Unmanaged content outside Lucy's managed scope is not a deletion target.
- `install` does **not** mean "delete everything not in manifest".
- `install` only synchronizes Lucy-managed scope; manual or ignored content stays
  outside that boundary unless the operator changes the manifest.

## Side-effect matrix

| Command | Mutates manifest | Mutates lockfile | Mutates runtime | Inspects observed state |
| --- | --- | --- | --- | --- |
| `add` | yes | yes | no | yes |
| `remove` | yes | yes | no | yes |
| `install` | no | yes | yes | yes |

## `lucy add`

### Contract

`add` is an explicit operator action that inserts or upgrades `required`
intent, then resolves the resulting closure.

### Manifest side effects

- Insert a new package into manifest intent as `required` when missing.
- Upgrade the existing `required` intent for the addressed package when present.
- Do not rewrite unrelated `required` roots without the operator asking for it.
- Do not convert `ignored` content into managed intent.

### Lockfile side effects

- Re-resolve the dependency closure implied by the updated manifest.
- Persist exact resolved facts for both `required` roots and resulting
  `transitive` dependencies.
- Record exact versions, source/provider decisions, hashes, install paths, and
  provenance.

### Runtime side effects

- None by contract.
- `add` may inspect the current server for compatibility or drift warnings, but
  managed runtime synchronization belongs to `install`.

## `lucy remove`

### Contract

`remove` is an explicit operator action that deletes `required` intent, then
prunes no-longer-needed `transitive` dependencies.

### Manifest side effects

- Remove the addressed package from `required` intent.
- Leave unrelated `required` roots unchanged unless the operator explicitly
  removes them.
- Leave `ignored` entries ignored.

### Lockfile side effects

- Re-resolve the closure after the required-intent removal.
- Prune exact lock entries for `transitive` packages that are no longer needed.
- Keep packages that remain reachable from another `required` root.

### Runtime side effects

- None by contract.
- `remove` may inspect observed state for warnings, but runtime deletion and
  reconciliation belong to `install`.

## `lucy install`

### Contract

`install` is the explicit convergence step: it synchronizes the current server
to manifest intent via exact lockfile facts.

### Manifest side effects

- None. `install` never changes desired intent.

### Lockfile side effects

- Materialize or refresh the exact resolved closure required by the current
  manifest.
- Use the lockfile as the exact fact layer for subsequent runtime sync.
- Do not widen the sync target beyond Lucy-managed scope.

### Runtime side effects

- Create or replace managed-scope artifacts whose exact runtime state differs
  from the lockfile.
- Prune managed-scope artifacts that are absent from the current lockfile.
- Respect `ignored` entries and unmanaged paths as hard boundaries.
- Never interpret the contract as permission to delete everything outside the
  manifest.

## Non-overlap summary

- `add` changes desired intent and recalculates exact facts.
- `remove` changes desired intent and recalculates exact facts.
- `install` applies exact facts to managed runtime state.

That separation keeps manifest editing, lockfile resolution, and runtime sync
from collapsing into one ambiguous command.
