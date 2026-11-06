#!/usr/bin/env bash
set -euo pipefail

# Writes the canonical .cipher_key: the binding flag plus four fragments.
# The CurseForge key travels on stdin only, so it never reaches a process argv
# and no fragment value is printed.

cd "$(dirname "$0")/.." || exit 1

if [ -z "${CURSEFORGE_API_KEY:-}" ]; then
  printf "[ERROR] CURSEFORGE_API_KEY is not set\n" >&2
  exit 1
fi

# Tagged releases bind the material to version and commit. Empty means unbound.
if [ -n "${CIPHER_RELEASE_VERSION:-}" ] && [ -n "${CIPHER_RELEASE_COMMIT:-}" ]; then
  :
elif [ -n "${CIPHER_RELEASE_VERSION:-}" ] || [ -n "${CIPHER_RELEASE_COMMIT:-}" ]; then
  printf "[ERROR] both CIPHER_RELEASE_VERSION and CIPHER_RELEASE_COMMIT must be set together\n" >&2
  exit 1
fi

bound=false
if [ -n "${CIPHER_RELEASE_VERSION:-}" ]; then
  bound=true
fi

gen_args=(-encrypt-stdin)
if [ "$bound" = "true" ]; then
  gen_args+=(-version "${CIPHER_RELEASE_VERSION}" -commit "${CIPHER_RELEASE_COMMIT}")
fi

out="$(printf "%s" "${CURSEFORGE_API_KEY}" | go run ./cmd/cipher "${gen_args[@]}")"
key_a="$(printf "%s\n" "$out" | sed -n "s/^cipher_key_a=//p")"
key_b="$(printf "%s\n" "$out" | sed -n "s/^cipher_key_b=//p")"
ciphertext_a="$(printf "%s\n" "$out" | sed -n "s/^cipher_ciphertext_a=//p")"
ciphertext_b="$(printf "%s\n" "$out" | sed -n "s/^cipher_ciphertext_b=//p")"
if [ -z "$key_a" ] || [ -z "$key_b" ] || [ -z "$ciphertext_a" ] || [ -z "$ciphertext_b" ]; then
  printf "[ERROR] cipher generator produced incomplete fragments\n" >&2
  exit 1
fi

{
  printf "cipher_bound=%s\n" "$bound"
  printf "cipher_key_a=%s\n" "$key_a"
  printf "cipher_key_b=%s\n" "$key_b"
  printf "cipher_ciphertext_a=%s\n" "$ciphertext_a"
  printf "cipher_ciphertext_b=%s\n" "$ciphertext_b"
} > .cipher_key
printf "[OK] Wrote .cipher_key (four fragments)\n"
