[
  (if .server.primary_runtime.Eco == "fabric" then empty else "identity.eco" end),
  (if .server.primary_runtime.Version == "0.16.9" then empty else "identity.version" end),
  (if ([.server.runtime_components[]? | select(.Eco == "minecraft") | .Version] | index("1.21.4")) then empty else "minecraft.version" end),
  (if ((.mod_path // []) | length) > 0 then empty else "mod_path.present" end)
]
