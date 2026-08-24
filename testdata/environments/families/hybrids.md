# Hybrid servers (Arclight, Luminol, Mohist)

Covers environments: `arclight-fabric`, `mohist`. (Youer is documented with the paper family.)

Hybrids run Bukkit/Spigot plugins on top of a mod loader platform. Detection relied on per-project launcher manifests; treat unresolved output as expected for some fixtures.

| Project  | Flavor                    | Upstream                                | Notes                                                                                                                                           |
| -------- | ------------------------- | --------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| Arclight | Fabric / Forge / NeoForge | <https://github.com/IzzelAliz/Arclight> | release tags look like `FeudalKings/1.0.1`; asset name carries `<mc>-<v>-<gitsha>`                                                              |
| Mohist   | Forge-hosted              | <https://mohistmc.com>                  | API `https://mohistmc.com/api/v2/projects/{project}/{mc}/builds/latest/download`; local sandbox jar predates identifiable metadata → manual pin |

Luminol is discontinued. Since we do not have an upstream any more, it will not be tested nor supported.

When re-pinning a hybrid artifact, prefer resolving the exact build first (match local sha256/md5 against upstream metadata) and flip the manifest entry from `manual:` to a pinned URL once confirmed.
