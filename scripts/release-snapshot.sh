#!/usr/bin/env bash
set -euo pipefail

# Builds a local GoReleaser snapshot. GoReleaser reads the fragments from the
# CIPHER_* environment, and bound material would tie a snapshot to a tag.

# shellcheck source=scripts/lib.sh
. "$(dirname "$0")/lib.sh"
cd "$(repo_root)"

load_cipher_key
if [ "${cipher_bound:-false}" = "true" ]; then
  printf "[ERROR] bound cipher material cannot be used for a snapshot; run task cipher:generate without CIPHER_RELEASE_*\n" >&2
  exit 1
fi

export CIPHER_KEY_A="${CIPHER_KEY_A:-${cipher_key_a:-}}"
export CIPHER_KEY_B="${CIPHER_KEY_B:-${cipher_key_b:-}}"
export CIPHER_CIPHERTEXT_A="${CIPHER_CIPHERTEXT_A:-${cipher_ciphertext_a:-}}"
export CIPHER_CIPHERTEXT_B="${CIPHER_CIPHERTEXT_B:-${cipher_ciphertext_b:-}}"

go tool goreleaser release --snapshot --clean --skip=publish
