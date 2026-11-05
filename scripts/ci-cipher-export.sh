#!/usr/bin/env bash
set -euo pipefail

# Generates cipher material bound to the tagged release, masks every fragment,
# and exports the four CIPHER_* variables for GoReleaser.

# shellcheck source=scripts/lib.sh
. "$(dirname "$0")/lib.sh"
cd "$(repo_root)"

: "${GITHUB_ENV:?GITHUB_ENV is not set}"
: "${GITHUB_REF_NAME:?GITHUB_REF_NAME is not set}"

# Seal-time identity: the tag without its leading v, and the checked-out commit.
version="${GITHUB_REF_NAME#v}"
commit="$(git rev-parse HEAD)"
export CIPHER_RELEASE_VERSION="$version"
export CIPHER_RELEASE_COMMIT="$commit"
task cipher:generate

key_a="$(cipher_fragment cipher_key_a)"
key_b="$(cipher_fragment cipher_key_b)"
ciphertext_a="$(cipher_fragment cipher_ciphertext_a)"
ciphertext_b="$(cipher_fragment cipher_ciphertext_b)"
if [ -z "$key_a" ] || [ -z "$key_b" ] || [ -z "$ciphertext_a" ] || [ -z "$ciphertext_b" ]; then
  printf "cipher: incomplete fragments after generate\n" >&2
  exit 1
fi

printf "::add-mask::%s\n" "$key_a"
printf "::add-mask::%s\n" "$key_b"
printf "::add-mask::%s\n" "$ciphertext_a"
printf "::add-mask::%s\n" "$ciphertext_b"
{
  printf "CIPHER_KEY_A=%s\n" "$key_a"
  printf "CIPHER_KEY_B=%s\n" "$key_b"
  printf "CIPHER_CIPHERTEXT_A=%s\n" "$ciphertext_a"
  printf "CIPHER_CIPHERTEXT_B=%s\n" "$ciphertext_b"
} >> "$GITHUB_ENV"
