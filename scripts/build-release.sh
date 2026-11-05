#!/usr/bin/env bash
set -euo pipefail

# Cross-builds the published release binaries under release/.

# shellcheck source=scripts/lib.sh
. "$(dirname "$0")/lib.sh"
cd "$(repo_root)"

load_cipher_key
ldflags="-w -s $(lucy_ldflags)"
tags="release,docsarchive,nethttpomithttp2"

if [ ! -f .build/glamour/build.mod ]; then
  sh _patches/glamour/materialize.sh
fi

mkdir -p release
for target in linux/amd64 linux/arm64 windows/amd64 darwin/amd64 darwin/arm64; do
  goos="${target%%/*}"
  goarch="${target##*/}"
  ext=""
  if [ "$goos" = "windows" ]; then
    ext=".exe"
  fi
  GOOS="$goos" GOARCH="$goarch" go build -trimpath \
    -modfile=.build/glamour/build.mod \
    -ldflags "$ldflags" \
    -tags "$tags" \
    -o "release/lucy-${goos}-${goarch}${ext}"
done
