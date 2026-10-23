# Forge

Covers sandboxes: `forge-1122`, `forge-1201`, `forge-12111`.

## Versioning and endpoints

Forge versions are `<mc>-<build>` (1.12.2 → 14.x, 1.20.1 → 47.x, 1.21.11 → 61.x). Recommended/latest builds come from <https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json>; artifacts are from `https://maven.minecraftforge.net/net/minecraftforge/forge/<mc>-<build>/` (ls directory returns 401 but direct downloads work).

Maven publishes separate `installer`, `universal`, and (for current Forge branches) `shim` jars. The installer is a provisioning tool, not the server runtime. The sandbox generator runs the pinned installer with `--installServer` and requires the declared `entry_point` to exist afterwards. The installer produces the patched server jar, libraries, argument sidecars, `run.sh`/`run.bat`, and — on shim-era branches — the shim; it never produces an all-in-one Forge `server.jar`.

## Installed layout

`forge-1201` and `forge-12111` are installer output, not just copied Maven
artifacts:

```
forge-1.20.1-47.4.10-installer.jar
libraries/net/minecraftforge/forge/1.20.1-47.4.10/
  forge-1.20.1-47.4.10-universal.jar   # runtime library
  forge-1.20.1-47.4.10-server.jar      # patched server payload
  unix_args.txt  win_args.txt          # generated launch arguments
libraries/net/minecraft/server/1.20.1/
  server-1.20.1.jar
run.sh  run.bat  user_jvm_args.txt
```

`forge-12111` adds the shim era: the installer writes both
`forge-1.21.11-61.2.0-shim.jar` in the server root (used by `run.sh` for its
Java check) and the canonical copy under the version directory, plus
`forge-1.21.11-61.2.0-server.jar`, the universal runtime, and the argument
sidecars. Discovery selects the version-directory shim as the launch target;
the root shim is bootstrap support, not a second installation.

## Legacy standalone universal

`forge-1122` is the pre-1.13 layout: one official
`forge-1.12.2-14.23.5.2860-universal.jar` at the sandbox root, no installer,
no argument sidecars, no library tree, and no shim. The universal jar is the
server itself and declares:

```
Main-Class: net.minecraftforge.fml.relauncher.ServerLaunchWrapper
Tweak-Class: net.minecraftforge.fml.common.launcher.FMLTweaker
```

The generator verifies the pinned jar's `Main-Class` before publishing the
fixture. The filename supplies the identity tuple: Minecraft 1.12.2, Forge
14.23.5.2860.

## Entry points

- `forge-1122`: `forge-1.12.2-14.23.5.2860-universal.jar`
- `forge-1201`: `libraries/net/minecraftforge/forge/1.20.1-47.4.10/unix_args.txt`
- `forge-12111`: `libraries/net/minecraftforge/forge/1.21.11-61.2.0/forge-1.21.11-61.2.0-shim.jar`

A modern universal jar without generated arguments, a shim, or its own launch
main class is a library. The modern fixtures do not rely on the universal-jar
fallback; `forge-1122` is the genuine legacy standalone case.
