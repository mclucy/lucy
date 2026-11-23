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
	"strings"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/buildid"
)

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
// The key covers the Lucy build, so the manifest encoding needs no version.
type Snapshot struct {
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

// Key computes the recipe identity of a locked server: H(Lucy build, method,
// product, Minecraft version, core version, core digest, each input identity
// and digest in lock order). Input order is part of the recipe because a
// method can derive a classpath from it. The workspace directory, URLs,
// filenames, Java runtime, and package set are absent: they do not change the
// output bytes.
func Key(locked document.LockedServer) (cas.Digest, error) {
	if err := locked.Bootstrap.Validate(); err != nil {
		return cas.Digest{}, err
	}
	build, err := buildid.Identity()
	if err != nil {
		return cas.Digest{}, err
	}
	core, err := artifactReference("core artifact", locked.Artifact.Hashes)
	if err != nil {
		return cas.Digest{}, err
	}

	hasher := sha256.New()
	writeField(hasher, "lucy-bootstrap-snapshot")
	writeField(hasher, build)
	writeField(hasher, string(locked.Bootstrap))
	writeField(hasher, string(locked.Product))
	writeField(hasher, locked.Minecraft.String())
	// The method derives launch paths from the core version, so two locks
	// with the same core bytes but a different declared version generate
	// different launchers.
	writeField(hasher, locked.CoreVersion.String())
	writeField(hasher, core)
	for _, input := range locked.Inputs {
		reference, err := inputReference(input)
		if err != nil {
			return cas.Digest{}, err
		}
		writeField(hasher, string(input.Provider)+":"+input.ProjectID+"@"+input.Version.String())
		writeField(hasher, reference)
	}
	var key cas.Digest
	hasher.Sum(key[:0])
	return key, nil
}

// inputReference identifies one locked input. An embedded input is addressed by
// its parent installer's digest plus the entry name, so a lock that omits an
// embedded digest still has a stable recipe.
func inputReference(input catalog.Artifact) (string, error) {
	if input.Embedded != "" && !hasDigest(input.Hashes) {
		if err := document.SafeRelativePath(input.Embedded); err != nil {
			return "", fmt.Errorf("embedded bootstrap input %s: %w", input.ProjectID, err)
		}
		return "embedded:" + filepath.ToSlash(filepath.Clean(input.Embedded)), nil
	}
	return artifactReference("bootstrap input "+input.ProjectID, input.Hashes)
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
