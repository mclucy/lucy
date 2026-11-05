#!/usr/bin/env bash
set -euo pipefail

# Posts the release announcement to the Discord webhook. Discord rejects an embed
# description over 4096 characters, and the release body repeats installation
# instructions that the embed already carries, so the announcement keeps the
# changelog and links out for the rest.

: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is not set}"
: "${RUNNER_TEMP:?RUNNER_TEMP is not set}"
: "${DISCORD_WEBHOOK_URL:?DISCORD_WEBHOOK_URL is not set}"

tag="${INPUT_TAG:-${GITHUB_REF_NAME}}"
gh release view "${tag}" --repo "${GITHUB_REPOSITORY}" \
  --json body,url,isPrerelease > "${RUNNER_TEMP}/release.json"
jq -r '.body' "${RUNNER_TEMP}/release.json" > "${RUNNER_TEMP}/notes.md"
sed -n '/^## Installation$/q;p' \
  "${RUNNER_TEMP}/notes.md" > "${RUNNER_TEMP}/changelog.md"

# Mentions stay inert: changelog entries carry author handles, and an
# announcement must not ping the channel.
jq -n \
  --arg tag "${tag}" \
  --arg url "$(jq -r '.url' "${RUNNER_TEMP}/release.json")" \
  --argjson prerelease "$(jq -r '.isPrerelease' "${RUNNER_TEMP}/release.json")" \
  --rawfile changelog "${RUNNER_TEMP}/changelog.md" \
  '{
    allowed_mentions: { parse: [] },
    embeds: [{
      title: (if $prerelease then "lucy \($tag) · pre-release" else "lucy \($tag)" end),
      url: $url,
      color: (if $prerelease then 16436250 else 5793266 end),
      description: (
        ($changelog | sub("\\n+$"; ""))
        | if . == "" then
            "No user-visible changes."
          elif length > 3900 then
            .[0:3900] | sub("\\n[^\\n]*$"; "") + "\n\n[Full release notes](" + $url + ")"
          else . end
      ),
      fields: [
        { name: "Install", value: "`curl -fsSL https://lucy.lc/sh | sh`" }
      ],
      footer: { text: "lucy.lc" }
    }]
  }' > "${RUNNER_TEMP}/discord-payload.json"

status="$(curl -sS -o "${RUNNER_TEMP}/discord-response.txt" \
  -w '%{http_code}' \
  -H 'Content-Type: application/json' \
  --retry 3 --retry-delay 2 \
  --data-binary "@${RUNNER_TEMP}/discord-payload.json" \
  "${DISCORD_WEBHOOK_URL}")"
case "${status}" in
  200 | 204) printf "Announced %s to Discord\n" "${tag}" ;;
  *)
    printf "::error::Discord webhook returned HTTP %s\n" "${status}" >&2
    cat "${RUNNER_TEMP}/discord-response.txt" >&2
    exit 1
    ;;
esac
