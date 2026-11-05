#!/usr/bin/env bash
set -euo pipefail

# Builds the linux/amd64 container image from the release artifact and proves the
# binary starts inside it. GoReleaser stages the same layout for publishing.

cd "$(dirname "$0")/.." || exit 1

mkdir -p image/linux/amd64
gunzip -c release/lucy-linux-amd64.gz > image/linux/amd64/lucy
chmod 0755 image/linux/amd64/lucy
cp Dockerfile image/Dockerfile
docker buildx build --platform linux/amd64 --load --tag lucy:ci image
docker run --rm lucy:ci --version
