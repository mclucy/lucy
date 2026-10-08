# Fabric

Covers environments: `fabric-executable-1214`, `fabric-executable-262`, `fabric-installed-1214`, `fabric-installed-262`, `fabric-installed-114`.

## Layouts

- An executable Server (.jar) is self-contained launcher (`fabric-server-mc.<mc>-loader.<l>-launcher.<i>.jar`); downloads loader libraries on first run.
- A fabric installer (server mode) creates a shim `fabric-server-launch.jar` beside a separate vanilla `server.jar`.
- The legacy approach is everything bundled into one single launcher jar. There are no dependency libraries under `libraries/`

## Layouts

| Layout                                                                  | Key files                                                              | Loader version source                                      | Game version source                                                                                                                                                                                                                                |
|-------------------------------------------------------------------------|------------------------------------------------------------------------|------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Executable server jar (example: `fabric-executable-262`)                | `install.properties`                                                   | `install.properties` field `fabric-loader-version`         | `install.properties` field `game-version`                                                                                                                                                                                                          |
| Installer shim, old (installer ≤ 1.0, example: `fabric-installed-1214`) | `fabric-server-launch.properties` with `launch.mainClass` = KnotServer | `MANIFEST.MF` `Class-Path` entry `fabric-loader/<version>` | `MANIFEST.MF` `Class-Path` entry `intermediary/<mc version>`. If that entry is not present, `version.json` next to the jar, then `version.json` inside the jar named by `serverJar`.                                                               |
| Installer shim, new (installer ≥ 1.1, example: `fabric-installed-262`)  | `fabric-server-launch.properties` with `launch.mainClass` = KnotServer | `MANIFEST.MF` `Class-Path` entry `fabric-loader/<version>` | No clue from the artifact itself. Lucy reads `serverJar` from `fabric-server-launcher.properties` to locate the vanilla server. That entry names the vanilla jar. The default is `server.jar`. Lucy reads `version.json` field `id` from that jar. |

Note:

- loader ≥ 0.12 has a breaking change to us that renamed the Knot package (`impl.` inserted); both spellings are handled.
- `.fabric/`, `libraries/`, `versions/` cannot be used for detection as they are generated artifacts of the first run.
- A shim boots the jar named by `serverJar`. Lucy treats that jar as a component of the shim. Lucy does not report it as a standalone server. Without this rule, a directory with a shim and its vanilla jar reads as two servers side by side.

## Endpoints

- Meta: `https://meta.fabricmc.net/v2/versions/{game,loader,installer}`, launcher jar at `/v2/versions/loader/{game}/{loader}/{installer}/server/jar`
- Installer jars: `https://maven.fabricmc.net/net/fabricmc/fabric-installer/<v>/...`

References: <https://fabricmc.net/use/server>, <https://meta.fabricmc.net>, <https://wiki.fabricmc.net/documentation:fabric_loader>
