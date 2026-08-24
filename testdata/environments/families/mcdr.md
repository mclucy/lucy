# MCDR wrapper

Covers environments: `mcdr-fabric`, `mcdr-forge`, `mcdr-neoforge`.

MCDReforged (MCDR) is a Python management wrapper around a real Minecraft server. Its presence changes where lucy looks for state and plugins:

```
config.yml            # MCDR configuration (root)
permission.yml        # MCDR permission levels
plugins/*.pyz|*.mcdr  # MCDR plugins (zip-like archives)
logs/MCDR.log         # wrapper log marker
server/               # the REAL server directory (jar, eula.txt, libraries/)
```

The nested `server/` directory is what platform detectors analyze; the wrapper files are what make the environment "runnable" end-to-end for install/add testing.

The three wrappers cover one loader family each: Fabric 1.21.4 (`fabric_meta` launcher jar), Forge 1.20.1 (`forge-...-universal.jar` + installer under `server/libraries/`), NeoForge 20.2.93 (installer + universal). Plugins come from GitHub releases: PrimeBackup (`TISUnion/PrimeBackup`, `.pyz`) and WhereIs (`Lazy-Bing-Server/WhereIs-MCDR`, `.mcdr`).

Reference: <https://github.com/TISUnion/MCDReforged>
