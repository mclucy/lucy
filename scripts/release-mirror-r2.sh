#!/usr/bin/env bash
set -euo pipefail

# Mirrors a published release to R2 and publishes its completion metadata last.
# The metadata object is the promotion point: the Worker only considers versions
# that have one.

cd "$(dirname "$0")/.." || exit 1

: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is not set}"
: "${CLOUDFLARE_API_TOKEN:?CLOUDFLARE_API_TOKEN is not set}"
: "${CLOUDFLARE_ACCOUNT_ID:?CLOUDFLARE_ACCOUNT_ID is not set}"

tag="${INPUT_TAG:-${GITHUB_REF_NAME}}"
version="${tag#v}"
cf=assets/node_modules/.bin/cf
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

rm -rf release-assets release-meta
mkdir -p release-assets release-meta

echo "Downloading ${tag}"
gh release download "$tag" --repo "${GITHUB_REPOSITORY}" --dir release-assets

commit="$(
  git rev-parse --short=7 "${tag}^{commit}" 2>/dev/null ||
    gh api "repos/${GITHUB_REPOSITORY}/commits/${tag}" --jq '.sha[0:7]'
)"
published_at="$(
  gh release view "$tag" --repo "${GITHUB_REPOSITORY}" --json publishedAt --jq .publishedAt
)"

node scripts/release-manifest.mjs \
  --dir release-assets \
  --out "release-meta/${version}.json" \
  --tag "$tag" \
  --version "$version" \
  --repo "${GITHUB_REPOSITORY}" \
  --commit "$commit" \
  --published-at "$published_at"

npm ci --prefix assets --no-audit --no-fund

echo "Uploading artifacts for ${version}"
for file in release-assets/*; do
  name="$(basename "$file")"
  case "$name" in
    *.tar.gz) type=application/gzip ;;
    *.zip) type=application/zip ;;
    *) type=text/plain ;;
  esac
  "$cf" r2 objects put "v${version}/${name}" \
    --bucket-name lucy-releases --file "$file" --content-type "$type"
done

meta="release-meta/${version}.json"
echo "Promoting ${version}"
if "$cf" r2 objects get "meta/${version}.json" >"$tmp" 2>/dev/null; then
  if ! cmp -s "$tmp" "$meta"; then
    echo "meta/${version}.json already exists with different content" >&2
    exit 1
  fi
  echo "Metadata is already present and unchanged"
else
  "$cf" r2 objects put "meta/${version}.json" \
    --bucket-name lucy-releases --file "$meta" --content-type application/json
  echo "Published metadata for ${version}"
fi
