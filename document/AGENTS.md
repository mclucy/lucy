# document

Package `document` owns the persisted server specification files: the manifest declaration (`lucy.yaml`), the lock resolution (`lucy-lock.yaml`), and their YAML codec.

## Responsibilities

- **Manifest Declaration (`lucy.yaml`)**: Records user intent, version selectors, server product definitions, tracked file policies, and package requirements grouped by ecosystem.
- **Lock Resolution (`lucy-lock.yaml`)**: Records fully resolved server inputs, pinned artifacts, requirement bindings, bootstrap inputs, and the `manifest_fingerprint` linking back to the declaration.
- **State Boundary**: Owns declarative intent and resolution only. Committed installation state belongs in `receipt` (`.lucy/install.yaml`), while live runtime observation belongs in `workspace`.

## Canonicalization & Codec Boundaries

- **Semantic Fingerprints**: Document `Fingerprint()` methods produce a `sha256:<hex>` digest over deterministic JSON of normalized content.
  - Manifest fingerprint excludes the server name so renaming does not invalidate resolution.
  - Lock fingerprint normalizes declaration order and canonical forms so formatting variations do not change identity.
- **Comment-Preserving Codec**: `Commenter` renders fresh documents with schema directives and documentation comments. Its `Merge` path updates existing YAML trees while preserving user comments and filling empty comment slots from struct tags.
- **Path Safety**: Normalization and validation enforce safe relative paths and reject path traversal before persistence.

## Relevant Tests

- `document/document_manifest_test.go`: Manifest decoding, normalization, and fuzzing.
- `document/document_lock_test.go`: Lock replay persistence and requirement binding fuzzing.
- `document/document_lock_fingerprint_test.go`: Deterministic canonical lock fingerprinting.
- `document/document_manifest_java_test.go`: Java version and distribution normalization.
