# Forge

Covers environments: `forge-1201`, `forge-12111`.

## Versioning and endpoints

Forge versions are `<mc>-<build>` (1.20.1 → 47.x, 1.21.11 → 61.x). Recommended/latest builds come from <https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json>; artifacts are from `https://maven.minecraftforge.net/net/minecraftforge/forge/<mc>-<build>/` (ls directory returns 401 but direct downloads work).

Maven publishes `installer`, `universal`, and (since Forge 61) `shim` jars. `server.jar` and arg files are generated later by running `java -jar forge-<mc>-<b>-installer.jar --installServer` therefore would not be stable approach to detection.

## Shims (Forge ≥ 61, MC 1.21.11+)

Since Forge 61 an extra `forge-<mc>-<b>-shim.jar` is published. It contains `bootstrap-shim.properties` + `bootstrap-shim.list`. Pre-61 installs have no shim.

## Installed layout

```
libraries/net/minecraftforge/forge/<mc>-<b>/
  forge-<mc>-<b>-universal.jar     # manifest carries ForgeVersion/GameVersion
  forge-<mc>-<b>-server.jar        # installer-produced; siblings unix_args.txt / win_args.txt
  forge-<mc>-<b>-shim.jar          # shim era only
  unix_args.txt  win_args.txt
run.sh  run.bat  user_jvm_args.txt  eula.txt
```

Detection methods:

- Direct hash check on the artifact against maven
- Offline unpack checks, fallback approach for modified or user-compiled artifacts (universal manifest attributes; server jar sibling args files).
