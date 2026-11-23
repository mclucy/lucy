package receipt

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/mclucy/lucy/bootstrap/snapshot"
	"github.com/mclucy/lucy/document"
	"go.yaml.in/yaml/v4"
)

// BootstrapFingerprint returns the bootstrap installation key of a lock: the
// snapshot recipe key plus the server directory. The recipe key names a
// shareable output tree; the directory is where this workspace installs it, so
// moving the server restages the runtime. A new Lucy build changes the recipe
// key, so the next install restages. Packages, tracked files, MCDR, Java
// requirements, URLs, and filenames cannot invalidate an unchanged bootstrap.
func BootstrapFingerprint(l *document.Lock) (string, error) {
	if l == nil {
		return "", nil
	}
	recipe, err := snapshot.Key(l.Server)
	if err != nil {
		return "", err
	}
	data, err := yaml.Marshal(struct {
		Recipe    string `yaml:"recipe"`
		Directory string `yaml:"directory"`
	}{recipe.String(), l.Server.Directory})
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
