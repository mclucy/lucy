#!/usr/bin/env bash
set -euo pipefail

# Plans the e2e matrix for the reusable runner. DOMAINS and COMPILE_FULL come
# from the change detector; the compile fixture list comes from the repository,
# so the matrix and a local `go test ./e2e/compile` can never disagree about
# which fixtures exist.

cd "$(dirname "$0")/.." || exit 1

: "${DOMAINS:?DOMAINS is not set}"
: "${GITHUB_OUTPUT:?GITHUB_OUTPUT is not set}"

compile_full="${COMPILE_FULL:-false}"
fixtures="[]"
if [ "$compile_full" = "true" ]; then
  fixtures="$(find e2e/testdata/repo -maxdepth 1 -type f -name "*.yaml" -exec basename {} .yaml \; | sort | jq -Rsc 'split("\n") | map(select(length > 0))')"
  if [ "$(jq "length" <<<"$fixtures")" -eq 0 ]; then
    printf "no compile fixtures found\n" >&2
    exit 1
  fi
fi

suites="$(cat <<'EOF'
[
  {
    "domain": "status",
    "group": "status",
    "suite": "status",
    "command": "go test -tags e2e -count=1 -v ./e2e/status -run 'TestProbe/(arclight-fabric|arclight-forge|cobblemon-1211|create-plus-1211|fabric-executable-1214|forge-1201|mcdr-fabric|neoforge-262|paper)'",
    "timeout": 45,
    "sandboxes": "arclight-fabric,arclight-forge,cobblemon-1211,create-plus-1211,fabric-executable-1214,forge-1201,mcdr-fabric,neoforge-262,paper",
    "java": "",
    "gradle": false,
    "cache-key": "",
    "cache-pattern": ""
  },
  {
    "domain": "init",
    "group": "init",
    "suite": "init",
    "command": "go test -tags e2e -count=1 -v ./e2e/init -run 'TestInitialize/(cobblemon-1211|create-plus-1211)'",
    "timeout": 30,
    "sandboxes": "cobblemon-1211,create-plus-1211",
    "java": "",
    "gradle": false,
    "cache-key": "",
    "cache-pattern": ""
  },
  {
    "domain": "install",
    "group": "install",
    "suite": "install",
    "command": "go test -tags e2e -count=1 -timeout 28m -parallel 2 -v ./e2e/install -run '^TestInstall$'",
    "timeout": 30,
    "sandboxes": "vanilla-1201,forge-1201,forge-12111,neoforge-211,paper,purpur,cobblemon-1211",
    "java": "21",
    "gradle": false,
    "cache-key": "",
    "cache-pattern": ""
  },
  {
    "domain": "install",
    "group": "init-install",
    "suite": "init-install",
    "command": "go test -tags e2e -count=1 -timeout 28m -parallel 2 -v ./e2e/install -run '^TestInitInstall$'",
    "timeout": 30,
    "sandboxes": "vanilla-1201,forge-1201,forge-12111,paper,cobblemon-1211",
    "java": "21",
    "gradle": false,
    "cache-key": "",
    "cache-pattern": ""
  }
]
EOF
)"

matrix="$(jq -c \
  --argjson domains "$DOMAINS" \
  --argjson fixtures "$fixtures" \
  --argjson full "$compile_full" \
  '[
    .[] | select(.domain as $domain | $domains | index($domain)) | del(.domain)
  ] + (
    if (($domains | index("compile")) != null) or $full then
      if $full then
        [
          $fixtures[] | . as $fixture | {
            "group": "compile",
            "suite": $fixture,
            "command": ("go test -tags e2e -count=1 -timeout 40m ./e2e/compile -run \"^TestCompile/" + $fixture + "$\""),
            "timeout": 45,
            "sandboxes": "",
            "java": "",
            "gradle": true,
            "cache-key": $fixture,
            "cache-pattern": ("e2e/testdata/repo/" + $fixture + ".yaml")
          }
        ]
      else
        [
          {
            "group": "compile",
            "suite": "smoke",
            "command": "LUCY_COMPILE_LAYER=smoke go test -tags e2e -count=1 -timeout 55m -parallel 3 ./e2e/compile",
            "timeout": 60,
            "sandboxes": "",
            "java": "",
            "gradle": true,
            "cache-key": "smoke",
            "cache-pattern": "e2e/testdata/repo/*.yaml"
          }
        ]
      end
    else
      []
    end
  )' <<<"$suites")"
printf "matrix=%s\n" "$matrix" >> "$GITHUB_OUTPUT"
