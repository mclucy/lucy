package document_test

import (
	"testing"

	documentpkg "github.com/mclucy/lucy/document"
	javapkg "github.com/mclucy/lucy/java"
)

// TestFingerprintIsCanonical covers the inputs that validation normalizes.
// Fingerprint identifies a lock's content, so two spellings of one lock must
// fingerprint alike, just as the lock a publisher writes does.
func TestFingerprintIsCanonical(t *testing.T) {
	fingerprint := func(document *documentpkg.Lock) string {
		t.Helper()
		value, err := document.Fingerprint()
		if err != nil {
			t.Fatalf("lockfile.Fingerprint() error = %v", err)
		}
		return value
	}

	baseline := lockDocument(".", "config/server.properties", "1.0.0", ">=1.0", true)
	baseline.Server.Java.Distribution = javapkg.DistributionTemurin
	canonical := fingerprint(baseline)
	for _, spelling := range []javapkg.Distribution{"TEMURIN", "Temurin", "temurin"} {
		document := lockDocument(".", "config/server.properties", "1.0.0", ">=1.0", true)
		document.Server.Java.Distribution = spelling
		if got := fingerprint(document); got != canonical {
			t.Fatalf("fingerprint with distribution %q = %s, want %s", spelling, got, canonical)
		}
	}

	ordered := lockDocument(".", "config/server.properties", "1.0.0", ">=1.0", true)
	ordered.Files = []documentpkg.TrackedFile{{Path: "config/a.txt"}, {Path: "config/b.txt"}}
	reordered := lockDocument(".", "config/server.properties", "1.0.0", ">=1.0", true)
	reordered.Files = []documentpkg.TrackedFile{{Path: "config/b.txt"}, {Path: "config/a.txt"}}
	if fingerprint(ordered) != fingerprint(reordered) {
		t.Fatal("file declaration order changed the lock fingerprint")
	}

	root := t.TempDir()
	unspelled := lockDocument(".", "config/server.properties", "1.0.0", ">=1.0", true)
	unspelled.Server.Java.Distribution = javapkg.Distribution("TEMURIN")
	before := fingerprint(unspelled)
	if err := documentpkg.LockFile(root).Write(unspelled); err != nil {
		t.Fatalf("lockfile.Write() error = %v", err)
	}
	decoded, err := documentpkg.LockFile(root).Read()
	if err != nil {
		t.Fatalf("lockfile.Read() error = %v", err)
	}
	if after := fingerprint(decoded); after != before {
		t.Fatalf("fingerprint before persistence = %s, after = %s", before, after)
	}
}
