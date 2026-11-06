package install

import (
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/receipt"
)

// A receipt that does not own a required managed bootstrap output must not
// claim currency, even when the receipt's fingerprint and the file itself are
// intact. Otherwise the output is never restaged and never reported unowned.
func TestBootstrapCurrentRequiresReceiptOwnershipOfManagedOutput(t *testing.T) {
	root := t.TempDir()
	core := []byte("plain server core\n")
	transactionTestWrite(t, root, "server/server.jar", core)
	lock := &document.Lock{
		SchemaVersion: document.LockSchemaVersion,
		Server: document.LockedServer{
			Minecraft: "1.21.4",
			Directory: "server",
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderMojang,
				ProjectID: "server",
				Version:   "1.21.4",
				Filename:  "server.jar",
				Hashes:    map[string]string{"sha256": transactionTestDigest(core)},
			},
		},
	}
	fingerprint, err := receipt.BootstrapFingerprint(lock)
	if err != nil {
		t.Fatalf("bootstrap fingerprint: %v", err)
	}

	partial := receipt.New()
	partial.BootstrapFingerprint = fingerprint
	current, err := bootstrapCurrent(root, lock, &partial, nil)
	if err != nil {
		t.Fatalf("partial receipt check: %v", err)
	}
	if current {
		t.Fatal("receipt without an entry for server/server.jar reported current")
	}

	owned := receipt.New()
	owned.BootstrapFingerprint = fingerprint
	owned.Entries = []receipt.Entry{{
		Path:   "server/server.jar",
		SHA256: transactionTestDigest(core),
		Origin: receipt.OriginBootstrap,
	}}
	current, err = bootstrapCurrent(root, lock, &owned, nil)
	if err != nil {
		t.Fatalf("owned receipt check: %v", err)
	}
	if !current {
		t.Fatal("receipt owning server/server.jar with its baseline reported not current")
	}
}
