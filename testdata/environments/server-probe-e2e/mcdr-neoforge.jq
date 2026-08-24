[
  (if .server.primary_runtime.Eco == "neoforge" then empty else "identity.eco" end),
  (if .server.primary_runtime.Version == "20.2.93" then empty else "identity.version" end),
  (if ([.server.runtime_components[]? | select(.Eco == "minecraft") | .Version] | index("1.20.2")) then empty else "minecraft.version" end),
  (if ((.mod_path // []) | length) > 0 then empty else "mod_path.present" end),
  (if (.mod_path // [] | first | endswith("/server/mods")) then empty else "mod_path.nested" end)
]
