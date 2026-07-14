# Cipher key operations

Lucy embeds a CurseForge API key in public release binaries so the CLI can call
the CurseForge API without requiring every user to supply their own key.

## Threat model (intentional)

- The embedded key is **recoverable** by a determined local attacker who reverse
  engineers the binary.
- The design raises extraction effort (split fragments, per-release material,
  AEAD identity binding). It does **not** claim confidentiality against a
  motivated extractor.
- Do not treat this as a substitute for server-side secrets or short-lived tokens.

## Per-release material

- Each tagged release generates **fresh** key and ciphertext fragments from
  `CF_API_KEY` at release time.
- Rebuilds of the same tag are **not** bit-identical for cipher material: a new
  generate step produces a new random seed and nonce.
- Local/dev builds and GoReleaser snapshots use **unbound** material (empty
  release version and commit associated data).

## Runtime binding

Tagged binaries embed:

1. Four linker fragments (`keyA`, `keyB`, `ciphertextA`, `ciphertextB`)
2. Public release identity (`releaseVersion`, `releaseCommit`)

Decryption uses XChaCha20-Poly1305 with associated data:

```text
lucy-cipher-v1 \0 <version> \0 <commit>
```

Identity must be both empty (local) or both nonempty (tagged). A mismatch
between seal-time and open-time identity fails decryption.

## Rotation

1. Update the repository secret `CF_API_KEY` to the new CurseForge key.
2. Cut a **new** release tag so CI regenerates fragments and publishes binaries
   bound to that tag's version and commit.
3. Old public binaries retain the previous embedded material until users upgrade.

## Emergency response

If a key is abused or leaked:

1. Revoke or rotate the key in the CurseForge account/dashboard (provider-side).
2. Update `CF_API_KEY` in CI secrets.
3. Publish a new Lucy release so new binaries embed the rotated key.
4. Communicate upgrade guidance as needed; old binaries cannot be remotely wiped.

## Provider controls (unverified here)

CurseForge may offer key restrictions, monitoring, or rate controls. Those
capabilities have **not** been verified from official documentation in this
repository. Confirm any reliance on IP allowlists, app-name binding, usage
alerts, or similar controls directly in the CurseForge developer dashboard or
with CurseForge support before treating them as part of the security posture.

## Local generation

```bash
export CF_API_KEY=...   # never commit; process env only (never argv)
task cipher:generate    # feeds key on stdin to cmd/cipher -encrypt-stdin
task build:dev          # injects four fragments; empty release identity
```

Tagged CI sets `CIPHER_RELEASE_VERSION` (tag without leading `v`) and
`CIPHER_RELEASE_COMMIT` (`git rev-parse HEAD`, full hash) before `task cipher:generate`.
GoReleaser injects the same full hash via `{{ .FullCommit }}` so seal-time and
open-time AD match. CI masks all four fragment values before exporting them.

`task release:snapshot` rejects material marked `cipher_bound=true`; regenerate
without `CIPHER_RELEASE_*` before creating a local snapshot.

This runbook lives under `docs/shared/` so it is tracked (other `docs/*` paths
are local-only per `.gitignore`).
