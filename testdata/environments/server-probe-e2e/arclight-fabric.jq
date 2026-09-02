[
  (if .server.primary_runtime.Name == "arclight" then empty else "identity.name" end),
  (if .server.primary_runtime.Version == "arclight-1.21.1-1.0.1-8ec9529" then empty else "identity.version" end),
  (if ([.server.runtime_components[]? | select(.Eco == "fabric") | .Version] | index("0.16.14")) then empty else "loader.version" end),
  (if ([.server.runtime_components[]? | select(.Eco == "minecraft") | .Version] | index("1.21.1")) then empty else "minecraft.version" end),
  (if ((.mod_path // []) | length) > 0 then empty else "mod_path.present" end)
]
