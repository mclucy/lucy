package receipt

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/mclucy/lucy/bootstrap/snapshot"
	"github.com/mclucy/lucy/document"
	"go.yaml.in/yaml/v4"
)

// BootstrapFingerprint returns the bootstrap recipe key of a lock: the same
// key the bootstrap snapshot cache uses, so a receipt answers exactly the
// snapshot a fresh staging would look up. A new Lucy build changes the key,
// so the next install restages the runtime. Packages, tracked files, MCDR,
// and Java requirements cannot invalidate an unchanged bootstrap.
func BootstrapFingerprint(l *document.Lock) (string, error) {
	if l == nil {
		return "", nil
	}
	key, err := snapshot.Key(l.Server)
	if err != nil {
		return "", err
	}
	return key.Hex(), nil
}

// PackageFingerprint digests the package pins and wrapper runtime in the lock:
// package identities, artifacts, requirement bindings, and MCDR directory.
func PackageFingerprint(l *document.Lock) (string, error) {
	if l == nil {
		return "", nil
	}
	data, err := yaml.Marshal(struct {
		MCDR     *document.LockedMCDR `yaml:"mcdr,omitempty"`
		Packages []document.Package   `yaml:"packages"`
	}{l.MCDR, l.Packages})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
