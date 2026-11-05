#!/usr/bin/env bash
set -euo pipefail

# Reports whether CurseForge accepts the configured API key. A key that is only
# used locally may still be valid, so a missing key warns instead of failing.

if [ -z "${CURSEFORGE_API_KEY:-}" ]; then
  printf "[WARN] CURSEFORGE_API_KEY is not set, so CurseForge API calls will fail\n"
  exit 0
fi

status="$(curl -s -o /dev/null -w "%{http_code}" \
  -H "Accept: application/json" \
  -H "x-api-key: ${CURSEFORGE_API_KEY}" \
  "https://api.curseforge.com/v1/games?pageSize=1" 2>/dev/null || echo "000")"
case "$status" in
  200) printf "[OK] CURSEFORGE_API_KEY verified against CurseForge API\n" ;;
  401) printf "[WARN] CURSEFORGE_API_KEY rejected by CurseForge API (HTTP 401)\n" ;;
  000) printf "[WARN] Could not reach CurseForge API (network error)\n" ;;
  *) printf "[WARN] CurseForge API returned unexpected status %s\n" "$status" ;;
esac
