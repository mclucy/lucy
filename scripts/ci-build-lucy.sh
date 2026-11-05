#!/usr/bin/env bash
set -euo pipefail

# Builds Lucy for an e2e job. Generates cipher material when CI carries a
# CurseForge key.

cd "$(dirname "$0")/.." || exit 1

if [ -n "${CURSEFORGE_API_KEY:-}" ]; then
  task cipher:generate
fi
task dev
