# Vanilla

Covers environments: `vanilla-1201`, `vanilla-262`; inner cores of `fabric-installed-1214` (1.21.4), `fabric-installed-114` (1.14) see the [./fabric.md](fabric docs).

## Resolving the server jar

Mojang has no stable per-version direct URLs; resolve at generation time:

1. `https://piston-meta.mojang.com/mc/game/version_manifest_v2.json`
2. find version entry by id → its `url` (per-version JSON)
3. `downloads.server.url` (+ `sha1`) → piston-data download

envgen does this for `mojang_version:` artifacts and verifies the manifest sha256 afterwards.

## Format eras

Modern vanilla jars use the "bundler" bootstrap format (`META-INF/libraries.list`, bootstrap main class), which the detector must distinguish from paperclip bundles.
