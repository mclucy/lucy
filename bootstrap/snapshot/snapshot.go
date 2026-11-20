// Package snapshot identifies reusable bootstrap output trees and describes
// their contents independently of any one workspace. It computes the recipe
// key, validates a manifest, and defines the storage contract that the
// artifact store implements. It never decides ownership, runs a bootstrap
// method, or encodes a method's file layout.
package snapshot

import (
	"context"
	"crypto/sha256"
	"fmt"
	"hash"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/server"
)

// FormatVersion is the manifest encoding and key derivation version. Increment
// it when either changes, so a stale manifest never answers a new lookup.
const FormatVersion = 1

// Entry describes one runtime-zone or seed-zone file of a bootstrap output.
// Policy and Role keep their bootstrap package words as opaque labels; this
// package does not interpret them.
type Entry struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Mode     uint32 `json:"mode"`
	Policy   string `json:"policy"`
	Role     string `json:"role"`
	Evidence string `json:"evidence,omitempty"`
}

// Snapshot is one stored bootstrap output set addressed by its recipe key.
type Snapshot struct {
	Version int        `json:"version"`
	Key     cas.Digest `json:"key"`
	Entries []Entry    `json:"entries"`
}

// Store persists snapshot manifests and their file contents. The artifact
// store implements it; installation wires it. A lookup miss is not an error.
type Store interface {
	LookupSnapshot(ctx context.Context, key cas.Digest) (Snapshot, bool, error)
	SaveSnapshot(ctx context.Context, snap Snapshot, sourceDir string) error
	RestoreSnapshot(ctx context.Context, snap Snapshot, destination string) error
}

// MethodRevision reports the generator revision of one bootstrap adapter.
// Increment a revision when a method produces different bytes for unchanged
// inputs, so old snapshots and receipts stop matching.
func MethodRevision(method server.BootstrapMethod) (int, error) {
	switch method {
	case server.BootstrapMethodPlain,
		server.BootstrapMethodPaperclip,
		server.BootstrapMethodFabric,
		server.BootstrapMethodQuilt,
		server.BootstrapMethodForgeInstaller:
		return 1, nil
	default:
		return 0, fmt.Errorf("unsupported bootstrap method %q", method)
	}
}

// Key computes the recipe identity of a locked server:
// H(method, method revision, Minecraft version, core digest, each input path
// and digest). The workspace directory, URLs, filenames, Java runtime, and
// package set are deliberately absent: they do not change the output bytes.
func Key(locked document.LockedServer) (cas.Digest, error) {
	revision, err := MethodRevision(locked.Bootstrapper)
	if err != nil {
		return cas.Digest{}, err
	}
	core, err := artifactReference("core artifact", locked.Artifact.Hashes)
	if err != nil {
		return cas.Digest{}, err
	}
	inputs := make([]server.BootstrapInput, len(locked.Inputs))
	copy(inputs, locked.Inputs)
	slices.SortFunc(inputs, func(a, b server.BootstrapInput) int {
		return strings.Compare(a.Path, b.Path)
	})

	hasher := sha256.New()
	writeField(hasher, "lucy-bootstrap-snapshot")
	writeField(hasher, fmt.Sprint(FormatVersion))
	writeField(hasher, string(locked.Bootstrapper))
	writeField(hasher, fmt.Sprint(revision))
	writeField(hasher, string(locked.Product))
	writeField(hasher, locked.Minecraft.String())
	// The adapter derives launch paths from the core version, so two locks
	// with the same core bytes but a different declared version generate
	// different launchers.
	writeField(hasher, locked.CoreVersion.String())
	writeField(hasher, core)
	for i, input := range inputs {
		if err := document.SafeRelativePath(input.Path); err != nil {
			return cas.Digest{}, fmt.Errorf("bootstrap input %d path: %w", i, err)
		}
		reference, err := inputReference(input)
		if err != nil {
			return cas.Digest{}, err
		}
		writeField(hasher, filepath.ToSlash(filepath.Clean(input.Path)))
		writeField(hasher, reference)
	}
	var key cas.Digest
	hasher.Sum(key[:0])
	return key, nil
}

// inputReference identifies one locked input. An embedded input is addressed by
// its parent installer's digest plus the entry name, so a lock that omits an
// embedded digest still has a stable recipe.
func inputReference(input server.BootstrapInput) (string, error) {
	if input.Embedded != "" && !hasDigest(input.Artifact.Hashes) {
		if err := document.SafeRelativePath(input.Embedded); err != nil {
			return "", fmt.Errorf("embedded bootstrap input %s: %w", input.Path, err)
		}
		return "embedded:" + filepath.ToSlash(filepath.Clean(input.Embedded)), nil
	}
	return artifactReference("bootstrap input "+input.Path, input.Artifact.Hashes)
}

func hasDigest(hashes map[string]string) bool {
	for _, algorithm := range []string{"sha256", "sha512", "sha1", "md5"} {
		if strings.TrimSpace(hashes[algorithm]) != "" {
			return true
		}
	}
	return false
}

// artifactReference names the strongest digest a lock pins for one artifact.
// SHA-256 is preferred, and a weaker pin still yields a stable identity rather
// than silently dropping the input from the key.
func artifactReference(name string, hashes map[string]string) (string, error) {
	for _, algorithm := range []string{"sha256", "sha512", "sha1", "md5"} {
		value := strings.ToLower(strings.TrimSpace(hashes[algorithm]))
		if value != "" {
			return algorithm + ":" + value, nil
		}
	}
	return "", fmt.Errorf("%s has no digest to identify the bootstrap recipe", name)
}

func writeField(hasher hash.Hash, value string) {
	fmt.Fprintf(hasher, "%d:", len(value))
	_, _ = hasher.Write([]byte(value))
	_, _ = hasher.Write([]byte{'\n'})
}

// Validate rejects a manifest whose entries could escape the destination,
// collide, or name content that is not addressed by SHA-256.
func (s Snapshot) Validate() error {
	if s.Version != FormatVersion {
		return fmt.Errorf("unsupported snapshot version %d", s.Version)
	}
	if _, err := cas.ParseDigest(s.Key.String()); err != nil {
		return fmt.Errorf("invalid snapshot key: %w", err)
	}
	seen := make(map[string]bool, len(s.Entries))
	for i, entry := range s.Entries {
		if err := ValidateEntry(entry); err != nil {
			return fmt.Errorf("snapshot entry %d: %w", i, err)
		}
		if seen[entry.Path] {
			return fmt.Errorf("snapshot entry %d repeats path %q", i, entry.Path)
		}
		seen[entry.Path] = true
	}
	return nil
}

// ValidateEntry rejects one entry that cannot be restored safely.
func ValidateEntry(entry Entry) error {
	path := filepath.FromSlash(entry.Path)
	if entry.Path == "" || entry.Path == "." || filepath.IsAbs(path) ||
		filepath.ToSlash(filepath.Clean(path)) != entry.Path {
		return fmt.Errorf("path %q is not a canonical server-relative path", entry.Path)
	}
	if err := document.SafeRelativePath(path); err != nil {
		return err
	}
	if _, err := cas.ParseDigest("sha256:" + strings.ToLower(entry.SHA256)); err != nil {
		return fmt.Errorf("path %q has an invalid sha256 digest %q", entry.Path, entry.SHA256)
	}
	if entry.Mode > 0o777 {
		return fmt.Errorf("path %q has unsupported mode %#o", entry.Path, entry.Mode)
	}
	return nil
}
