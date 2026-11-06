#!/usr/bin/env bash
set -euo pipefail

# Publishes the rendered wiki mirror. The badge directory stays: README.md links
# to badge/coverage.svg in the wiki, and that badge is published elsewhere.

: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is not set}"
: "${GITHUB_TOKEN:?GITHUB_TOKEN is not set}"
: "${GITHUB_SHA:?GITHUB_SHA is not set}"
: "${RUNNER_TEMP:?RUNNER_TEMP is not set}"

wiki="${RUNNER_TEMP}/lucy.wiki"
git clone --depth 1 "https://github.com/${GITHUB_REPOSITORY}.wiki.git" "$wiki"

find "$wiki" -mindepth 1 -maxdepth 1 ! -name .git ! -name badge -exec rm -rf {} +
cp -R "${RUNNER_TEMP}/wiki/." "$wiki/"

cd "$wiki"
git config user.name "github-actions[bot]"
git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
git add -A
if git diff --cached --quiet; then
  printf "Wiki is already up to date.\n"
  exit 0
fi
git commit -m "docs: mirror documentation from ${GITHUB_SHA::7}"
git push "https://x-access-token:${GITHUB_TOKEN}@github.com/${GITHUB_REPOSITORY}.wiki.git" HEAD:master
