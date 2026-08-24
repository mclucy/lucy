# Fabric

Covers environments: `fabric-executable-1214`, `fabric-executable-262`, `fabric-installed-1214`, `fabric-installed-114`.

## Layouts

- A executable Server (.jar) is self-contained launcher (`fabric-server-mc.<mc>-loader.<l>-launcher.<i>.jar`); downloads loader libraries on first run.
- A fabric installer (server mode) creates a shim `fabric-server-launch.jar` beside a separate vanilla `server.jar`.
- The legacy approach is everything bundled into one fat launcher jar.

## Layouts and what lucy keys on

| Layout                | Marker inside jar                                                                                                                                                                                                                                                                  |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Executable Server jar | `install.properties`                                                                                                                                                                                                                                                               |
| Installer stub        | `fabric-server-launch.properties`: `launch.mainClass=net.fabricmc.loader.impl.launch.knot.KnotServer` (legacy spelling without `.impl.` also accepted) + MANIFEST `Class-Path:` into `libraries/net/fabricmc/{intermediary,fabric-loader}/<ver>/`; sidecar `version.json` fallback |

Note:

- loader ≥ 0.12 has a breaking change to us that renamed the Knot package (`impl.` inserted); both spellings are handled.
- `.fabric/`, `libraries/`, `versions/` cannot be used for detection as they are artifacts of the first run.

## Endpoints

- Meta: `https://meta.fabricmc.net/v2/versions/{game,loader,installer}`, launcher jar at `/v2/versions/loader/{game}/{loader}/{installer}/server/jar`
- Installer jars: `https://maven.fabricmc.net/net/fabricmc/fabric-installer/<v>/...`

References: <https://fabricmc.net/use/server>, <https://meta.fabricmc.net>, <https://wiki.fabricmc.net/documentation:fabric_loader>
