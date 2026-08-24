# Paper family (paperclip-based servers)

Covers environments: `paper`, `folia`, `purpur`, `divinemc`, `leaf`, `leaves`, `reaper`, `youer`.

## What the jar is

A "paperclip" jar is a small bootstrap: its real payload (vanilla + patches + libraries) downloads on first run. The jar itself carries the evidence that matters to lucy's detector:

- `META-INF/MANIFEST.MF` — `Main-Class: io.papermc.paperclip.Main` (some forks ship `io.papermc.paperclip.Paperclip`)
- `META-INF/main-class` — downstream craftbukkit main class
- `META-INF/libraries.list` / `versions.list` / `patches.list` / `download-context` — bundle manifests
- `version.json` — `id` is the target Minecraft version
- fork markers live inside `libraries.list` entries, e.g. `dev.folia:folia-api:*`, `org.leavesmc.leaves:leaves-api:*`, `io.papermc:paper:*`

Fork quirks:
- **leaves**: extra `META-INF/build-info` (`Leaves\t<mc>\t<build>`) and `META-INF/leavesclip-version`
- **reaper** (1.12.2 era): `patch.properties` with `patch=paperMC.patch`, legacy paperclip main class
- **youer** (MohistMC): manifest `Implementation-Vendor: MohistMC`, `Main-Class: com.mohistmc.launcher.youer.Main`

## Versioning quirks

Paper-family build numbers are independent of MC versions (`paper-1.21.11-130.jar` = MC 1.21.11, build 130). Purpur distributes plain `server.jar`; identify builds by md5 from its API.

## Download endpoints

| Source | Endpoint | Notes |
|---|---|---|
| Paper/Folia | `https://fill.papermc.io/v3/projects/{p}/versions/{mc}/builds/{b}` | **v2 API retired Aug 2026**; descriptive `User-Agent` required; `downloads["server:default"].url`; fill-data object URLs embed the sha256 |
| Purpur | `https://api.purpurmc.org/v2/purpur/{mc}/{build}` (+ `/download`) | build metadata exposes `md5` |
| Leaves | GitHub releases `LeavesMC/Leaves`, tag `<mc>-<build>-<sha>` | |
| DivineMC, Leaf, Reaper, Youer | hosting varies/unstable | currently `manual:` pins in the manifest |

References: <https://docs.papermc.org/misc/downloads-service>, <https://fill.papermc.io>, <https://api.purpurmc.org>, <https://github.com/LeavesMC/Leaves>
