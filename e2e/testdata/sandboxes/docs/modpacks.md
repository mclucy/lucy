# Modpacks

Covers sandboxes: `cobblemon-1211`, `create-plus-1211`.

## What the sandbox contains

Each sandbox pins one Modrinth pack archive with `modpack: true`. The sandbox
tool reads `modrinth.index.json`, downloads every file whose `env.server` is
`required` or `optional` to its declared path, and unpacks `overrides/` and
`server-overrides/` at the root. Create+ 6.0.0 Alpha f has one entry with
`unknown` side metadata, which the pack format does not define; the tool skips
it until the upstream pack is corrected. The pinned loader installer then runs
with `--installServer`, so the generated argument sidecars, patched server jar,
and libraries land beside the pack files. `create-plus-1211` launches through
`libraries/net/neoforged/neoforge/21.1.233/unix_args.txt`; its universal jar is
a runtime library, not an entry point.

| Sandbox          | Pack                                        | Loader           | Server files |
| ---------------- | ------------------------------------------- | ---------------- | ------------ |
| cobblemon-1211   | Cobblemon Official Modpack [Fabric] 1.8.2   | Fabric 0.19.5    | 34 mods      |
| create-plus-1211 | Create+ 6.0.0 Alpha f                       | NeoForge 21.1.233 | 105 mods    |

Generation verifies each downloaded file against the sha512 in the pack
index. The pack archive itself is pinned by sha256 in the manifest.

## Initialization

`TestInitialize` clones each sandbox, runs `lucy init --no-interactive`, and
checks the manifest. The initializer identifies the loader from its artifacts
and resolves every mod jar by hash, so the generated package list is the
translation of the pack. Client-only jars are left out.

## Install fixture

`e2e/install/testdata/cobblemon-1211.yaml` translates part of the Cobblemon pack
into a Lucy manifest. Every entry names the Modrinth slug and the exact pack
version. The pinned versions match the `TestInitialize` output.
`cobblemon-1211.packages.yaml` pins the project, release, file, and digest
that install must produce.

The lock subset leaves out two groups:

- jars that the pack marks as client-only, and jars whose `fabric.mod.json`
  declares `environment: client`, because a server install rejects them;
- jars whose dependency graph exceeds the resolver candidate budget. Module
  names in this group collide with unrelated Modrinth projects, such as the
  `melody` module of FancyMenu and the `melody` project slug.

Add an excluded jar back when the resolver can place it. The probe case runs
on the full pack and does not depend on the lock subset.

## Endpoints

- Modrinth: <https://modrinth.com/modpack/cobblemon-fabric>,
  <https://modrinth.com/modpack/create_plus>
