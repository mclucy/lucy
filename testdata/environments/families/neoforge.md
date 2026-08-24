# NeoForge

Covers environments: `neoforge-211`, `neoforge-262`.

## Versioning and endpoints

Classic lines drop the MC `1.` prefix and append the build (`20.2.x` → MC 1.20.2, `21.1.x` → MC 1.21.1); since the MC year scheme the full triple is kept and a build segment appended (`26.2.0.67` → MC 26.2.0, build 67). Latest per line comes from `https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml`; there is no promotions service.

Maven publishes `installer`, `universal`, `userdev`, and `sources` jars — never `server.jar` or args files; those are produced by running `java -jar neoforge-<v>-installer.jar --installServer`.

## Installed layout eras

```
libraries/net/neoforged/neoforge/<v>/
  neoforge-<v>-universal.jar       # manifest: FML-System-Mods: neoforge
  neoforge-<v>-server.jar          # dual-jar era only
  unix_args.txt  win_args.txt
run.sh  run.bat  user_jvm_args.txt  eula.txt
```

Verified by live installs: `21.1.248` ships both jars; later in the 21.x cycle the server jar was dropped in favor of `libraries/net/neoforged/minecraft-server-patched/<v>/` (`26.2.0.67` has none). Detection handles both — the universal jar alone is sufficient (manifest check offline, maven hash check online), so a pinned universal makes generated environments detection-complete without running the installer.
