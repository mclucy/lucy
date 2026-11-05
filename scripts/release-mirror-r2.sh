#!/usr/bin/env bash
set -euo pipefail

# Mirrors the published release artifacts to the R2 bucket that serves lucy.lc.

cd "$(dirname "$0")/.." || exit 1

: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is not set}"

tag="${INPUT_TAG:-${GITHUB_REF_NAME}}"
version="${tag#v}"
mkdir -p release-assets
gh release download "$tag" --repo "${GITHUB_REPOSITORY}" --dir release-assets
npm ci --prefix assets --no-audit --no-fund
for file in release-assets/*; do
  name="$(basename "$file")"
  case "$name" in
    *.tar.gz) type=application/gzip ;;
    *.zip) type=application/zip ;;
    *) type=text/plain ;;
  esac
  assets/node_modules/.bin/cf r2 objects put "v${version}/${name}" \
    --bucket-name lucy-releases --file "$file" --content-type "$type"
done
