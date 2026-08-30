[
  (if .server.primary_runtime.Eco == "minecraft" then empty else "identity.eco" end),
  (if .server.primary_runtime.Version == "1.20.1" then empty else "identity.version" end),
  (if ([.server.runtime_components[]? | select(.Eco == "minecraft") | .Version] | index("1.20.1")) then empty else "minecraft.version" end)
]
