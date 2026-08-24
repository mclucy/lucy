[
  (if .server.primary_runtime.Eco == "neoforge" then empty else "identity.eco" end),
  (if .server.primary_runtime.Version == "26.2.0.67" then empty else "identity.version" end),
  (if ([.server.runtime_components[]? | select(.Eco == "minecraft") | .Version] | index("1.26.2")) then empty else "minecraft.version" end),
  (if ((.mod_path // []) | length) > 0 then empty else "mod_path.present" end)
]
