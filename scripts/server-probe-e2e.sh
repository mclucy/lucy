#!/usr/bin/env bash
# Verifies generated sandbox environments against e2e jq programs.
#
# Usage: scripts/server-probe-e2e.sh <ids>   # comma- or space-separated
#
# Each id must have a fixture at testdata/environments/server-probe-e2e/<id>.jq. The
# fixture is a jq program reading `lucy status --json` on stdin and emitting
# the list of failed check names; an empty array means the environment probed
# exactly as expected.
#
# Requires .sandboxes/<id>/ to be generated (task envs:gen) and dist/lucy to
# resolve to a built binary (task build:dev).

set -uo pipefail

E2E=testdata/environments/server-probe-e2e
fail=0

# Ids may arrive as separate words or comma-joined lists (mirrors envs:gen --only);
# normalize to one flat word list before iterating.
set -- $(printf '%s ' "$@" | tr ',' ' ')

for id in "$@"; do
  dir=".sandboxes/$id"
  fixture="$E2E/$id.jq"

  if [ ! -d "$dir" ]; then
    echo "FAIL $id: not generated ($dir missing)"
    fail=1
    continue
  fi
  if [ ! -f "$fixture" ]; then
    echo "FAIL $id: no e2e program ($fixture missing)"
    fail=1
    continue
  fi

  result=$(
    cd "$dir" &&
      ../../dist/lucy status --json --no-style 2>/dev/null |
      jq -c -f "../../$fixture"
  )

  if [ "$result" = "[]" ]; then
    echo "ok   $id"
  else
    echo "FAIL $id: $result"
    fail=1
  fi
done

if [ "$fail" -ne 0 ]; then
  echo "verification failed"
fi
exit "$fail"
