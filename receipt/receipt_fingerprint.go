package receipt

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/mclucy/lucy/document"
	"gopkg.in/yaml.v3"
)

// BootstrapFingerprint digests the server-only resolution fields that determine
// bootstrap execution and its output contract: product, method, core artifact,
// inputs, and directory. Excludes Java requirement, tracked files, MCDR, and
// packages so that package-only or environment changes do not invalidate an
// unchanged bootstrap.
func BootstrapFingerprint(l *document.Lock) (string, error) {
	if l == nil {
		return "", nil
	}
	server := l.Server
	server.Java = document.LockedJava{}
	data, err := yaml.Marshal(struct {
		Server document.LockedServer `yaml:"server"`
	}{server})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
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
