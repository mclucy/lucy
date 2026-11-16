# Quilt

Covers sandboxes: `quilt-modpack-1201`.

## Layout

Quilt is a Fabric-compatible mod loader. A Quilt server installation keeps the
vanilla server JAR beside a generated launcher:

- `server.jar` is the Mojang server the loader patches at launch.
- `libraries/org/quiltmc/quilt-loader/<version>/quilt-loader-<version>.jar` is
  the loader.
- `quilt-server-launch.jar` is a generated manifest-only launcher. Its
  `Main-Class` is `org.quiltmc.loader.impl.launch.server.QuiltServerLauncher`
  and its embedded `quilt-server-launch.properties` names
  `org.quiltmc.loader.impl.launch.knot.KnotServer` as the server main class.
- `quilt-server-launcher.properties` selects the vanilla JAR with
  `serverJar=server.jar`.

The launcher JAR also carries the loader and Quilt profile libraries on its
`Class-Path`, so `java -jar quilt-server-launch.jar nogui` starts the server
without `-cp` arguments. The adapter generates these files; it never renames
the loader JAR into a launcher.

`quilt-modpack-1201` is a real Modrinth pack, Quilt Minims 1.0.0. Its index
declares Minecraft 1.20.1 and `quilt-loader` 0.20.0, and the sandbox tool
materializes its server files. The loader and vanilla JAR are downloaded by the
install under test rather than committed to the sandbox, so the sandbox proves
the pack translation while the lifecycle test proves the generated runtime.
Twelve of the pack's 26 mods are client-only, so an adopted server carries 14
package entries.

## Endpoints

- Meta profile: `https://meta.quiltmc.org/v3/versions/loader/{game}/{loader}/server/json`
- Loader JAR: `https://maven.quiltmc.org/repository/release/org/quiltmc/quilt-loader/<v>/quilt-loader-<v>.jar`

References:

- <https://quiltmc.org/en/install/server/>
- <https://meta.quiltmc.org>
