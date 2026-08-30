[
  (if .server.primary_runtime.Eco == "minecraft" then empty else "identity.eco" end),
  (if .server.primary_runtime.Version == "26.2" then empty else "identity.version" end),
  (if ([.server.runtime_components[]? | select(.Eco == "minecraft") | .Version] | index("26.2")) then empty else "minecraft.version" end)
]
