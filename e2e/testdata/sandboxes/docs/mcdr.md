# MCDR wrapper

Covers sandboxes: `mcdr-fabric`, `mcdr-forge`, `mcdr-neoforge`.

MCDReforged (MCDR) is a Python management wrapper around a real Minecraft server. Its presence changes where lucy looks for state and plugins:

```
config.yml            # MCDR configuration (root)
permission.yml        # MCDR permission levels
plugins/*.pyz|*.mcdr  # MCDR plugins (zip-like archives)
logs/MCDR.log         # wrapper log marker
server/               # the REAL server directory (jar, eula.txt, libraries/)
```

The nested `server/` directory is what platform detectors analyze; the wrapper files are what make the sandbox "runnable" end-to-end for install/add testing.

The three wrappers cover one loader family each: Fabric 1.21.4 (`fabric_meta` launcher jar), Forge 1.20.1, and NeoForge 20.2.93. The Forge and NeoForge
nested servers are materialized by running the pinned official installer with
`--installServer` inside `server/`, so they contain the generated argument
sidecars, patched server jar, and libraries rather than only copied Maven
jars. Their universal jars are runtime libraries, not entry points:

- `mcdr-forge`: `server/libraries/net/minecraftforge/forge/1.20.1-47.4.10/unix_args.txt`
- `mcdr-neoforge`: `server/libraries/net/neoforged/neoforge/20.2.93/unix_args.txt`

Plugins come from GitHub releases: PrimeBackup (`TISUnion/PrimeBackup`, `.pyz`) and WhereIs (`Lazy-Bing-Server/WhereIs-MCDR`, `.mcdr`).

Reference: <https://github.com/TISUnion/MCDReforged>
