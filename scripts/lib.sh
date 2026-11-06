#!/usr/bin/env bash
# Helpers shared by the repository scripts. Source this file; do not execute it.

# Resolves the repository root from the calling script.
repo_root() {
  cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd
}

# Loads the canonical cipher fragments. A chained cipher:generate writes the
# file after Task has already read dotenv, so every build loads it itself.
load_cipher_key() {
  # shellcheck source=/dev/null
  if [ -f .cipher_key ]; then . ./.cipher_key; fi
}

# Prints the linker flags: release identity plus the four cipher fragments.
# Missing fragments expand empty, which keeps unbound local builds working.
lucy_ldflags() {
  local version commit
  version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
  commit="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
  printf "%s" "-X github.com/mclucy/lucy/cmd.version=${version} -X github.com/mclucy/lucy/cmd.commit=${commit}"
  printf " %s" \
    "-X github.com/mclucy/lucy/internal/cipher.keyA=${cipher_key_a:-}" \
    "-X github.com/mclucy/lucy/internal/cipher.keyB=${cipher_key_b:-}" \
    "-X github.com/mclucy/lucy/internal/cipher.ciphertextA=${cipher_ciphertext_a:-}" \
    "-X github.com/mclucy/lucy/internal/cipher.ciphertextB=${cipher_ciphertext_b:-}"
}

# Prints one fragment value from the canonical file cipher:generate wrote.
cipher_fragment() {
  sed -n "s/^$1=//p" .cipher_key
}
