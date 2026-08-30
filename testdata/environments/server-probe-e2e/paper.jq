[
  (if .server.primary_runtime.Name == "paper" then empty else "identity.name" end),
  (if ([.server.runtime_components[]? | select(.Eco == "minecraft") | .Version] | index("1.21.11")) then empty else "minecraft.version" end),
  (if ((.mod_path // []) | length) > 0 then empty else "mod_path.present" end),
  (if (.mod_path // [] | first | endswith("/plugins")) then empty else "mod_path.plugins" end)
]
