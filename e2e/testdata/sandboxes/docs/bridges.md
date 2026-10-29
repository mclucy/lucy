# Compatibility bridges (Kilt, Sinytra Connector)

Covers sandboxes: `fabric-kilt`, `neoforge-connector`.

Each bridge runs one loader and loads content written for another. The server
runtime stays unchanged, so Lucy reports the host loader and discovers the
bridge package from the mod directory.

## Kilt

Kilt is a Fabric mod that loads Forge and NeoForge mods inside a Fabric
instance. The fixture pins Minecraft 1.21.1 and these mods:

- Kilt 21.1.15, the bridge itself
- Fabric API, required by Kilt at runtime
- Fabric Language Kotlin, required by Kilt at runtime

Kilt bundles its remaining dependencies as nested jars, including the
`mm_kilt` mod that its metadata requires. Lucy reads the outer
`fabric.mod.json` and reports `fabric:kilt`.

Kilt is experimental. The fixture verifies detection only; it does not boot
the server.

## Sinytra Connector

Connector is a NeoForge mod that loads Fabric mods inside a NeoForge instance.
The fixture pins NeoForge 21.1.248, materializes its installer output with
`--installServer`, and launches through
`libraries/net/neoforged/neoforge/21.1.248/unix_args.txt`. It pins these mods:

- Connector 2.0.0-beta.17, the bridge itself
- Forgified Fabric API, the Fabric API port that Connector needs

The Connector jar carries its mod metadata in
`META-INF/jarjar/org.sinytra.connector-<version>-mod.jar`. Lucy reads the
nested jar and reports `neoforge:connector`. A server that reports that
package offers the Fabric ecosystem with degraded compatibility.

## Endpoints

- Kilt: <https://modrinth.com/mod/kilt>
- Connector: <https://modrinth.com/mod/connector>
- Forgified Fabric API: <https://modrinth.com/mod/forgified-fabric-api>

References: <https://github.com/KiltMC/Kilt>, <https://github.com/Sinytra/Connector>
