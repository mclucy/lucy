# Forge

Covers environments: `forge-1201`, `forge-12111`.

## Versioning and endpoints

Forge versions are `<mc>-<build>` (1.20.1 → 47.x, 1.21.11 → 61.x). Recommended/latest builds come from <https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json>; artifacts are from `https://maven.minecraftforge.net/net/minecraftforge/forge/<mc>-<build>/` (ls directory returns 401 but direct downloads work).

Maven publishes separate `installer`, `universal`, and (for current Forge branches) `shim` jars. The installer is a provisioning tool, not the server runtime. Modern Forge installs a shim, argument files, Minecraft libraries, and generated `run.sh`/`run.bat` launchers; it does not produce an all-in-one Forge `server.jar` launcher.

## Shims (current Forge branches)

Current Forge branches publish an extra `forge-<mc>-<b>-shim.jar` as the server executable. It contains `bootstrap-shim.properties` + `bootstrap-shim.list`. The pinned `1.20.1-47.4.10` fixture predates that artifact and therefore exercises the universal-jar fallback.

## Installed layout

```
libraries/net/minecraftforge/forge/<mc>-<b>/
  forge-<mc>-<b>-universal.jar     # Forge runtime/library artifact
  forge-<mc>-<b>-shim.jar          # current server entry point
  unix_args.txt  win_args.txt       # generated classpath/JVM arguments
libraries/net/minecraft/server/<mc>/
  server-<mc>-bundled.jar          # Minecraft's bundled server input
run.sh  run.bat  user_jvm_args.txt  eula.txt
```

Detection methods:

- Direct hash check on the artifact against maven
- Offline unpack checks, fallback approach for modified or user-compiled artifacts (universal manifest attributes).
