#!/usr/bin/env bash
set -euo pipefail

# Mirrors a published release to R2 and publishes its completion metadata last.
# The metadata object is the promotion point: the Worker only considers versions
# that have one. Existing metadata or artifacts are never replaced with
# different content.

cd "$(dirname "$0")/.." || exit 1

: "${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is not set}"
: "${CLOUDFLARE_API_TOKEN:?CLOUDFLARE_API_TOKEN is not set}"
: "${CLOUDFLARE_ACCOUNT_ID:?CLOUDFLARE_ACCOUNT_ID is not set}"

tag="${INPUT_TAG:-${GITHUB_REF_NAME}}"
version="${tag#v}"
bucket=lucy-releases
cf=assets/node_modules/.bin/cf
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

list_keys() {
  "$cf" r2 objects list --bucket-name "$bucket" --prefix "$1" --per-page 1000
}

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

meta="release-meta/${version}.json"
meta_key="meta/${version}.json"

if ! listing="$(list_keys "$meta_key")"; then
  echo "Could not read ${meta_key}" >&2
  exit 1
fi
if printf '%s' "$listing" | jq -e --arg key "$meta_key" '.[] | select(.key == $key)' >/dev/null 2>&1; then
  if ! "$cf" r2 objects get "$meta_key" --bucket-name "$bucket" >"$work/existing.json"; then
    echo "Could not read ${meta_key}" >&2
    exit 1
  fi
  if ! cmp -s "$work/existing.json" "$meta"; then
    echo "${meta_key} already exists with different content" >&2
    exit 1
  fi
  echo "Metadata is already present and unchanged"
  exit 0
fi

# No metadata exists yet, so this version has not been promoted. Refuse to
# overwrite an artifact that an earlier interrupted run left behind.
if ! existing="$(list_keys "v${version}/")"; then
  echo "Could not list v${version}/" >&2
  exit 1
fi
if [ -n "$existing" ] && [ "$existing" != "[]" ]; then
  for file in release-assets/*; do
    name="$(basename "$file")"
    expected="$(wc -c <"$file" | tr -d ' ')"
    actual="$(printf '%s' "$existing" | jq -r --arg key "v${version}/${name}" '.[] | select(.key == $key) | .size' 2>/dev/null || true)"
    if [ -n "$actual" ] && [ "$actual" != "$expected" ]; then
      echo "v${version}/${name} already exists with a different size" >&2
      exit 1
    fi
  done
fi

echo "Uploading artifacts for ${version}"
for file in release-assets/*; do
  name="$(basename "$file")"
  case "$name" in
    *.tar.gz) type=application/gzip ;;
    *.zip) type=application/zip ;;
    *) type=text/plain ;;
  esac
  "$cf" r2 objects put "v${version}/${name}" \
    --bucket-name "$bucket" --file "$file" --content-type "$type"
done

echo "Promoting ${version}"
"$cf" r2 objects put "$meta_key" \
  --bucket-name "$bucket" --file "$meta" --content-type application/json
echo "Published metadata for ${version}"
