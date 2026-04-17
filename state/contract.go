package state

// StateLayer identifies which conceptual state layer a fact belongs to.
// DesiredState expresses user intent, ResolvedState expresses Lucy's chosen
// exact closure, and ObservedState expresses live facts discovered from the
// environment.
type StateLayer string

const (
	// DesiredState is the intent layer. It answers "what should Lucy manage for
	// this project?" and is persisted primarily in .lucy/manifest.toml, with
	// policy modifiers sourced from .lucy/config.toml.
	DesiredState StateLayer = "desired"

	// ResolvedState is the fully chosen closure. It answers "what exact graph,
	// artifact identity, and provenance did Lucy resolve for the desired state?"
	// and is persisted in .lucy/lock.json.
	ResolvedState StateLayer = "resolved"

	// ObservedState is the probe layer. It answers "what does the current working
	// directory actually contain right now?" and stays outside the persistent
	// state files in v1.
	ObservedState StateLayer = "observed"
)

// StateFile identifies one persistent Lucy state file.
type StateFile string

const (
	// ConfigFile stores policy and defaults for this project. It may include
	// operator preferences, source or safety defaults, and command behavior
	// settings, but it must not declare desired package roots, exact artifact
	// hashes, download URLs, or observed runtime facts.
	ConfigFile StateFile = ".lucy/config.toml"

	// ManifestFile stores desired environment intent as JSON. It owns direct
	// roots, managed-scope declarations, and other descriptive statements about
	// what the project wants Lucy to converge toward. It must not contain
	// lockfile-only fields such as exact transitive closures, hashes, or exact
	// download URLs.
	ManifestFile StateFile = ".lucy/manifest.json"

	// LockFile stores the exact resolved graph and provenance for a manifest. It
	// owns exact versions, chosen sources, artifact identity, provenance chains,
	// and other reproducibility data. It must not become a dump of live probe
	// facts, user policy defaults, or procedural orchestration state.
	LockFile StateFile = ".lucy/lock.json"
)

// ExplicitOwnership maps a field class to the file that owns it.
//
// Field classes are intentionally coarse-grained. They define boundary classes
// such as "policy.defaults" or "artifact.hashes" rather than concrete schema
// fields, so the ownership contract can exist before v1 codecs and structs do.
type ExplicitOwnership map[string]StateFile

// DefaultOwnership returns the v1 Option C ownership contract.
func DefaultOwnership() ExplicitOwnership {
	return ExplicitOwnership{
		"policy.defaults":         ConfigFile,
		"policy.source-selection": ConfigFile,
		"policy.safety":           ConfigFile,
		"intent.direct-roots":     ManifestFile,
		"intent.managed-scope":    ManifestFile,
		"intent.environment":      ManifestFile,
		"resolution.graph":        LockFile,
		"resolution.provenance":   LockFile,
		"artifact.hashes":         LockFile,
		"artifact.download-urls":  LockFile,
	}
}

// OwnerOf reports which file owns a field class under the default v1 contract.
func OwnerOf(fieldClass string) (StateFile, bool) {
	owner, ok := DefaultOwnership()[fieldClass]
	return owner, ok
}
