# NeoForge

Covers sandboxes: `neoforge-211`, `neoforge-262`; `neoforge-connector` and
`create-plus-1211` reuse the same installer output on their own versions.

## Versioning and endpoints

Classic lines drop the MC `1.` prefix and append the build (`20.2.x` → MC 1.20.2, `21.1.x` → MC 1.21.1); since the MC year scheme the full triple is kept and a build segment appended (`26.2.0.67` → MC 26.2.0, build 67). Minecraft 26.1 and later require Java 25 to launch. Latest per line comes from `https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml`; there is no promotions service.

Maven publishes `installer`, `universal`, `userdev`, and `sources` jars — never `server.jar` or args files; those are produced by running `java -jar neoforge-<v>-installer.jar --installServer`. The sandbox generator runs that command with the pinned installer and verifies the declared `entry_point`.

## Installed layout eras

```
libraries/net/neoforged/neoforge/<v>/
  neoforge-<v>-universal.jar       # manifest: FML-System-Mods: neoforge
  neoforge-<v>-server.jar          # dual-jar era only
  unix_args.txt  win_args.txt      # generated launch arguments
run.sh  run.bat  user_jvm_args.txt
```

Verified by live installs: `21.1.248` ships both the universal and patched
server jar. Later in the 21.x cycle the server jar was dropped in favor of
`libraries/net/neoforged/minecraft-server-patched/<v>/`; `26.2.0.67` has no
`neoforge-26.2.0.67-server.jar`, only the universal runtime and the patched
Minecraft jar. Both layouts launch through `unix_args.txt`/`win_args.txt`.

The installer itself runs on Java 17 or newer. The `26.2.0.67` server still
requires Java 25 to launch; the sandbox only materializes the installation and
never starts it.

## Entry points

- `neoforge-211`: `libraries/net/neoforged/neoforge/21.1.248/unix_args.txt`
- `neoforge-262`: `libraries/net/neoforged/neoforge/26.2.0.67/unix_args.txt`
- `neoforge-connector`: `libraries/net/neoforged/neoforge/21.1.248/unix_args.txt`
- `create-plus-1211`: `libraries/net/neoforged/neoforge/21.1.233/unix_args.txt`

The universal jar is a runtime library in every current layout; it is never
the entry point. A pinned universal alone is not a complete installed server.
