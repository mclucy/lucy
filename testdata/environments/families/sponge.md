# Sponge

Covers environments: `sponge-vanilla`, `sponge-forge`, `sponge-neo`.

## Flavors

Sponge publishes three universal jars targeting different platforms, all from the same Maven repository (`https://repo.spongepowered.org/repository/maven-releases/org/spongepowered/`):

| Artifact | Platform |
|---|---|
| `spongevanilla` | standalone vanilla-based |
| `spongeforge` | Forge-hosted |
| `spongeneo` | NeoForge-hosted |

Filename grammar (<https://docs.spongepowered.org/stable/en/versions/filenames.html>): `{name}-{mc}[-{forgeBuild}]-{api}-universal.jar`, where `{api}` is the SpongeAPI generation (17.0.0 pairs with MC 1.21.10). Detection treats these as jar-only edge cases; unresolved topology output is expected for some.

## Provenance (2026-08)

The distribution landscape shifted: `repo.spongepowered.org` maven-releases now serves a slim ~54 KB launcher-style artifact under the old version paths, while the live download endpoints are `https://dl.spongepowered.org/spongevanilla`, `https://dl.spongepowered.org/spongeforge`, and `https://dl.spongepowered.org/spongeneo` — each currently resolving to a ~50 KB slim launcher too, not the multi-MB `-universal.jar` builds. The sandbox copies predate this change and stay pinned `manual:`; flip to pinned URLs only after an upstream download matches the recorded sha256.
