#!/usr/bin/env bash
set -euo pipefail

# Excludes a published release from latest selection without deleting its
# immutable artifacts. Removing the marker restores it.

cd "$(dirname "$0")/.." || exit 1

: "${CLOUDFLARE_API_TOKEN:?CLOUDFLARE_API_TOKEN is not set}"
: "${CLOUDFLARE_ACCOUNT_ID:?CLOUDFLARE_ACCOUNT_ID is not set}"

tag="${INPUT_TAG:-${GITHUB_REF_NAME}}"
version="${tag#v}"
cf=assets/node_modules/.bin/cf

if [ ! -x "$cf" ]; then
  npm ci --prefix assets --no-audit --no-fund
fi

if [ "${INPUT_UNREVOKE:-false}" = "true" ]; then
  "$cf" r2 objects delete "meta/${version}.revoked" --bucket-name lucy-releases --force >/dev/null 2>&1 || true
  echo "Restored ${version} to latest selection"
  exit 0
fi

if ! "$cf" r2 objects get "meta/${version}.json" >/dev/null 2>&1; then
  echo "No metadata for ${version}; mirror the release before revoking it" >&2
  exit 1
fi

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
printf '{"revoked_at":"%s","reason":"%s"}\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  "${INPUT_REASON:-manual revocation}" >"$tmp"
"$cf" r2 objects put "meta/${version}.revoked" \
  --bucket-name lucy-releases --file "$tmp" --content-type application/json
echo "Revoked ${version}; latest selection updates within 60 seconds"
