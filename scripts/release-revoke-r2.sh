#!/usr/bin/env bash
set -euo pipefail

# Excludes a published release from latest selection without deleting its
# immutable artifacts. Removing the marker restores it.

cd "$(dirname "$0")/.." || exit 1

: "${CLOUDFLARE_API_TOKEN:?CLOUDFLARE_API_TOKEN is not set}"
: "${CLOUDFLARE_ACCOUNT_ID:?CLOUDFLARE_ACCOUNT_ID is not set}"

tag="${INPUT_TAG:-${GITHUB_REF_NAME}}"
version="${tag#v}"
bucket=lucy-releases
cf=assets/node_modules/.bin/cf
marker="meta/${version}.revoked"

if [ ! -x "$cf" ]; then
  npm ci --prefix assets --no-audit --no-fund
fi

list_keys() {
  "$cf" r2 objects list --bucket-name "$bucket" --prefix "$1" --per-page 1000
}

if [ "${INPUT_UNREVOKE:-false}" = "true" ]; then
  if ! listing="$(list_keys "$marker")"; then
    echo "Could not read ${marker}" >&2
    exit 1
  fi
  if printf '%s' "$listing" | jq -e --arg key "$marker" '.[] | select(.key == $key)' >/dev/null 2>&1; then
    "$cf" r2 objects delete "$marker" --bucket-name "$bucket" --force
    echo "Restored ${version} to latest selection"
  else
    echo "${version} is not revoked"
  fi
  exit 0
fi

if ! listing="$(list_keys "meta/${version}.json")"; then
  echo "Could not read metadata for ${version}" >&2
  exit 1
fi
if ! printf '%s' "$listing" | jq -e --arg key "meta/${version}.json" '.[] | select(.key == $key)' >/dev/null 2>&1; then
  echo "No metadata for ${version}; mirror the release before revoking it" >&2
  exit 1
fi

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
printf '{"revoked_at":"%s","reason":"%s"}\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  "${INPUT_REASON:-manual revocation}" >"$tmp"
"$cf" r2 objects put "$marker" \
  --bucket-name "$bucket" --file "$tmp" --content-type application/json
echo "Revoked ${version}; latest selection updates within 60 seconds"
