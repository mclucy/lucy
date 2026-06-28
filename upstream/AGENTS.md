# upstream/

Provider boundary for remote package sources.

## POP Compatibility Resolution

Regular package providers are data adapters. They may call remote APIs and convert remote payloads into Lucy types, but they must not participate in local workspace compatibility decisions.

Required boundaries:

- Do not import or reference `workspace` from regular package providers such as Modrinth or CurseForge.
- Do not pass workspace structs, topology, server runtime, game version, or loader version into regular package providers.
- Do not implement local compatibility selection inside regular package providers. A provider may expose `ListVersions` data and may resolve exact or source-native selectors, but it must not decide what is best for the current server.
- `upstream.VersionCandidate` is the parameter passed to injected compatibility predicates. Keep it limited to compatibility facts: `Version`, `GameVersions`, and `Loaders`.
- Provider-returned metadata that is not needed by the compatibility predicate belongs on `upstream.VersionInfo` or another wrapper. For example, `ReleaseType` and `PublishedAt` are ordering facts for callers, not DI predicate inputs.

The intended flow is:

1. Provider returns remote version facts via `VersionLister`.
2. Caller builds an `upstream.CompatibilityFunc` from workspace state.
3. Caller filters `VersionInfo.Candidate` with that function.
4. Caller sorts or otherwise chooses the final version.
5. Provider fetches the concrete version.
