package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/receipt"
	"github.com/mclucy/lucy/server"
)

// A receipt with a matching bootstrap fingerprint but no bootstrap entry must
// not claim currency, even when the file itself is intact. Otherwise the output
// is never restaged and never reported unowned.
func TestBootstrapCurrentRequiresReceiptOwnershipOfManagedOutput(t *testing.T) {
	root := t.TempDir()
	core := []byte("plain server core\n")
	transactionTestWrite(t, root, "server/server.jar", core)
	lock := &document.Lock{
		SchemaVersion: document.LockSchemaVersion,
		Server: document.LockedServer{
			Product:   server.ProductMinecraft,
			Minecraft: "1.21.4",
			Directory: "server",
			Bootstrap: server.BootstrapMethodVanilla,
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

// A recorded seed proves only that the bootstrap produced the file. Deleting it
// must invalidate currentness so the next install writes it again, while an
// edit to its content stays user-owned.
func TestBootstrapCurrentRequiresRecordedSeeds(t *testing.T) {
	root := t.TempDir()
	core := []byte("plain server core\n")
	transactionTestWrite(t, root, "server/server.jar", core)
	transactionTestWrite(t, root, "server/user_jvm_args.txt", []byte("-Xmx1G\n"))
	lock := &document.Lock{
		SchemaVersion: document.LockSchemaVersion,
		Server: document.LockedServer{
			Product:   server.ProductMinecraft,
			Minecraft: "1.21.4",
			Directory: "server",
			Bootstrap: server.BootstrapMethodVanilla,
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
	owned := receipt.New()
	owned.BootstrapFingerprint = fingerprint
	owned.SeedPaths = []string{"server/user_jvm_args.txt"}
	owned.Entries = []receipt.Entry{{
		Path:   "server/server.jar",
		SHA256: transactionTestDigest(core),
		Origin: receipt.OriginBootstrap,
	}}
	current, err := bootstrapCurrent(root, lock, &owned, nil)
	if err != nil {
		t.Fatalf("current seed: %v", err)
	}
	if !current {
		t.Fatal("receipt with a present recorded seed reported not current")
	}

	transactionTestWrite(t, root, "server/user_jvm_args.txt", []byte("-Xmx4G\n"))
	current, err = bootstrapCurrent(root, lock, &owned, nil)
	if err != nil {
		t.Fatalf("edited seed: %v", err)
	}
	if !current {
		t.Fatal("edited seed content invalidated bootstrap currentness")
	}

	if err := os.Remove(filepath.Join(root, "server", "user_jvm_args.txt")); err != nil {
		t.Fatalf("remove seed: %v", err)
	}
	current, err = bootstrapCurrent(root, lock, &owned, nil)
	if err != nil {
		t.Fatalf("missing seed: %v", err)
	}
	if current {
		t.Fatal("receipt with a missing recorded seed reported current")
	}
}
