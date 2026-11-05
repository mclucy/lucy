#!/usr/bin/env bash
set -euo pipefail

# Commits the regenerated manifest schema back to the triggering branch.

: "${GITHUB_REF_NAME:?GITHUB_REF_NAME is not set}"

git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
git add assets/public/manifest.schema.json
if git diff --cached --quiet; then
  printf "Manifest schema is up to date.\n"
  exit 0
fi
git commit -m "chore(manifest): regenerate schema"
git push origin "HEAD:${GITHUB_REF_NAME}"
