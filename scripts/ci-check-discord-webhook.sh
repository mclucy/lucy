#!/usr/bin/env bash
set -euo pipefail

# Rejects a webhook that would fail mid-announcement, after a release is already
# published.

case "${DISCORD_WEBHOOK_URL:-}" in
  "")
    printf "::error::DISCORD_WEBHOOK_URL is required to announce the release\n" >&2
    exit 1
    ;;
  https://discord.com/api/webhooks/* | https://discordapp.com/api/webhooks/*) ;;
  *)
    printf "::error::DISCORD_WEBHOOK_URL is not a Discord webhook URL\n" >&2
    exit 1
    ;;
esac
