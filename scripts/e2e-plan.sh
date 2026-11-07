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
    "domain": "mutation",
    "group": "mutation",
    "suite": "smoke",
    "command": "LUCY_HEURISTIC_LAYER=smoke go test -tags e2e -count=1 -timeout 40m -parallel 3 -v ./e2e/mutation",
    "timeout": 45,
    "sandboxes": "",
    "java": "",
    "gradle": false,
    "cache-key": "",
    "cache-pattern": "",
    "report-artifact": "lucy-heuristic-mutation-smoke"
  },
  {
    "domain": "init",
    "group": "init",
    "suite": "init",
    "command": "go test -tags e2e -count=1 -timeout 25m -v ./e2e/init -run 'TestInitialize/(cobblemon-1211|create-plus-1211)'",
    "timeout": 30,
    "sandboxes": "cobblemon-1211,create-plus-1211",
    "java": "",
    "gradle": false,
    "cache-key": "",
    "cache-pattern": "",
    "report-artifact": ""
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
            "cache-pattern": ("e2e/testdata/repo/" + $fixture + ".yaml"),
            "report-artifact": ""
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
            "cache-pattern": "e2e/testdata/repo/*.yaml",
            "report-artifact": ""
          }
        ]
      end
    else
      []
    end
  )' <<<"$suites")"
printf "matrix=%s\n" "$matrix" >> "$GITHUB_OUTPUT"
