package receipt

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/mclucy/lucy/document"
	"gopkg.in/yaml.v3"
)

// ResolutionFingerprint digests the lock fields a receipt pins: the server,
// MCDR and package fields only. Tracked files, the manifest fingerprint, and
// the Java requirement stay out. Changing those fields does not change
// installed files or invalidate the receipt.
func ResolutionFingerprint(l *document.Lock) (string, error) {
	server := l.Server
	server.Java = document.LockedJava{}
	data, err := yaml.Marshal(struct {
		Server   document.LockedServer `yaml:"server"`
		MCDR     *document.LockedMCDR  `yaml:"mcdr,omitempty"`
		Packages []document.Package    `yaml:"packages"`
	}{server, l.MCDR, l.Packages})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
