package cmd

// CommandContract defines the durable semantic boundary for a user-facing
// command. These contracts are intentionally stricter than the current
// implementation so future work can converge on one meaning per command.
type CommandContract struct {
	Name            string
	Summary         string
	MutatesManifest bool
	MutatesLockfile bool
	MutatesRuntime  bool
	ObservesRuntime bool

	ManifestEffect string
	LockfileEffect string
	RuntimeEffect  string

	Guardrails []string
}

var (
	addContract = CommandContract{
		Name:            "add",
		Summary:         "Insert or upgrade required intent, then resolve the resulting closure.",
		MutatesManifest: true,
		MutatesLockfile: true,
		MutatesRuntime:  false,
		ObservesRuntime: true,
		ManifestEffect:  "Insert a missing package as required intent or upgrade the existing required intent for the addressed package.",
		LockfileEffect:  "Resolve the full closure implied by the manifest after the required-intent change and record exact versions, sources, hashes, install paths, and provenance.",
		RuntimeEffect:   "No direct runtime synchronization contract. Runtime drift may be observed for compatibility checks and warnings, but runtime reconciliation belongs to install.",
		Guardrails: []string{
			"Must not delete unmanaged content.",
			"Must not treat ignored entries as required or transitive.",
			"Must not use add as a generic sync/apply command.",
		},
	}

	removeContract = CommandContract{
		Name:            "remove",
		Summary:         "Remove required intent, then prune transitive dependencies that are no longer needed.",
		MutatesManifest: true,
		MutatesLockfile: true,
		MutatesRuntime:  false,
		ObservesRuntime: true,
		ManifestEffect:  "Remove the addressed package from required intent only; ignored entries remain ignored, and unrelated required roots remain intact.",
		LockfileEffect:  "Re-resolve the closure after the required-intent removal and prune no longer needed transitive dependencies while keeping packages still reachable from another required root.",
		RuntimeEffect:   "No direct runtime synchronization contract. Runtime drift may be inspected for warnings, but file deletion/application belongs to install.",
		Guardrails: []string{
			"Must not remove ignored content.",
			"Must not delete still-required packages or still-needed transitives.",
			"Must not claim ownership of unmanaged runtime files.",
		},
	}

	installContract = CommandContract{
		Name:            "install",
		Summary:         "Synchronize managed runtime state to manifest intent using exact lockfile facts.",
		MutatesManifest: false,
		MutatesLockfile: true,
		MutatesRuntime:  true,
		ObservesRuntime: true,
		ManifestEffect:  "None. Install never rewrites desired intent.",
		LockfileEffect:  "Materialize or refresh the exact resolved closure needed to satisfy the current manifest, then use that lockfile as the source of truth for managed runtime sync.",
		RuntimeEffect:   "Create, replace, or prune only managed-scope runtime artifacts whose exact state differs from the lockfile. Ignored and unmanaged content are observed boundaries, not deletion targets.",
		Guardrails: []string{
			"Must not mean delete everything not mentioned in the manifest.",
			"Must not mutate ignored entries or unmanaged paths.",
			"Must treat probe output as observed state for drift detection, not as manifest intent.",
		},
	}
)

func CommandContracts() map[string]CommandContract {
	return map[string]CommandContract{
		addContract.Name:     addContract,
		removeContract.Name:  removeContract,
		installContract.Name: installContract,
	}
}
