package cache

import "time"

// EntryKind distinguishes artifact entries (large, immutable binaries like
// server JARs and mods) from metadata entries (small, frequently refreshed
// manifests and version indexes).
type EntryKind uint8

const (
	KindMetadata EntryKind = iota
	KindArtifact
)

func (k EntryKind) String() string {
	switch k {
	case KindMetadata:
		return "metadata"
	case KindArtifact:
		return "artifact"
	default:
		return "unknown"
	}
}

// HashAlgorithm identifies the hash function used for integrity verification.
// Different upstream sources provide different algorithms: Mojang uses SHA-1,
// Modrinth provides SHA-1 and SHA-512, and Lucy uses SHA-256 internally for
// content addressing.
type HashAlgorithm uint8

const (
	HashNone   HashAlgorithm = iota
	HashSHA1                 // Mojang-provided hashes
	HashSHA256               // Internal content addressing
	HashSHA512               // Modrinth-provided hashes
)

func (h HashAlgorithm) String() string {
	switch h {
	case HashSHA1:
		return "sha1"
	case HashSHA256:
		return "sha256"
	case HashSHA512:
		return "sha512"
	default:
		return "none"
	}
}

// ParseHashAlgorithm converts a string representation to HashAlgorithm.
// Returns HashNone for unrecognized inputs.
func ParseHashAlgorithm(s string) HashAlgorithm {
	switch s {
	case "sha1":
		return HashSHA1
	case "sha256":
		return HashSHA256
	case "sha512":
		return HashSHA512
	default:
		return HashNone
	}
}

// IntegrityState tracks whether a cached entry's content has been verified
// against an expected digest from an upstream source.
type IntegrityState uint8

const (
	// IntegrityUnverified means the entry was cached without a known-good
	// digest to compare against, or verification has not yet occurred.
	IntegrityUnverified IntegrityState = iota

	// IntegrityVerified means the entry's content matched the expected
	// digest at cache-add time.
	IntegrityVerified
)

func (s IntegrityState) String() string {
	switch s {
	case IntegrityUnverified:
		return "unverified"
	case IntegrityVerified:
		return "verified"
	default:
		return "unknown"
	}
}

// Integrity holds the expected or actual digest of a cache entry along with
// its verification state. When Expected is empty, the entry operates in
// unverified mode (best-effort caching without integrity guarantees).
type Integrity struct {
	Algorithm HashAlgorithm  `json:"algorithm"`
	Expected  string         `json:"expected,omitempty"`
	Actual    string         `json:"actual,omitempty"`
	State     IntegrityState `json:"state"`
}

// CacheEntry is the enriched metadata record for a single cached blob.
// ContentHash (always SHA-256) is used for content-addressed storage.
// Integrity tracks the upstream-provided digest which may use a different
// algorithm (SHA-1 for Mojang, SHA-512 for Modrinth).
type CacheEntry struct {
	Kind        EntryKind `json:"kind"`
	Filename    string    `json:"filename"`
	Size        int64     `json:"size"`
	ContentHash string    `json:"content_hash"`
	Integrity   Integrity `json:"integrity"`
	Expiration  time.Time `json:"expiration"`
	Key         string    `json:"key"`
	CreatedAt   time.Time `json:"created_at"`
}
