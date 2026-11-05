#!/usr/bin/env bash
set -euo pipefail

# Builds the host-platform development binary and its stable dist/lucy symlink.

# shellcheck source=scripts/lib.sh
. "$(dirname "$0")/lib.sh"
cd "$(repo_root)"

goos="$(go env GOOS)"
goarch="$(go env GOARCH)"

load_cipher_key
ldflags="$(lucy_ldflags)"

if [ ! -f .build/glamour/build.mod ]; then
  sh _patches/glamour/materialize.sh
fi

mkdir -p dist
# HTTP/1.1 covers every provider; omitting HTTP/2 keeps binaries smaller.
GOOS="$goos" GOARCH="$goarch" go build \
  -modfile=.build/glamour/build.mod \
  -ldflags "$ldflags" \
  -tags nethttpomithttp2 \
  -o "dist/lucy-${goos}-${goarch}"
ln -sf "lucy-${goos}-${goarch}" dist/lucy
