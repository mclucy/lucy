package document_test

import (
	"testing"

	documentpkg "github.com/mclucy/lucy/document"
	javapkg "github.com/mclucy/lucy/java"
	serverpkg "github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
)

// TestJavaDistributionNormalizes covers the spelling of a declared Java
// distribution. Normalize decides what the manifest says, so two spellings of
// one distribution must normalize, persist, and fingerprint alike.
func TestJavaDistributionNormalizes(t *testing.T) {
	for _, spelling := range []string{"TEMURIN", "Temurin", " temurin "} {
		document := manifestWithJavaDistribution(spelling)
		if err := document.Normalize(); err != nil {
			t.Fatalf("Normalize(%q) error = %v", spelling, err)
		}
		if got := document.Server.Java.Distribution; got != javapkg.DistributionTemurin {
			t.Fatalf("Normalize(%q) distribution = %q, want %q", spelling, got, javapkg.DistributionTemurin)
		}
		normalized, err := document.Fingerprint()
		if err != nil {
			t.Fatalf("Fingerprint(%q) error = %v", spelling, err)
		}

		root := t.TempDir()
		if err := documentpkg.ManifestFile(root).Write(document); err != nil {
			t.Fatalf("Write(%q) error = %v", spelling, err)
		}
		decoded, err := documentpkg.ManifestFile(root).Read()
		if err != nil {
			t.Fatalf("Read(%q) error = %v", spelling, err)
		}
		if got := decoded.Server.Java.Distribution; got != javapkg.DistributionTemurin {
			t.Fatalf("Read(%q) distribution = %q, want %q", spelling, got, javapkg.DistributionTemurin)
		}
		persisted, err := decoded.Fingerprint()
		if err != nil {
			t.Fatalf("Fingerprint of read manifest for %q: %v", spelling, err)
		}
		if normalized != persisted {
			t.Fatalf(
				"manifest fingerprint for %q = %s before persistence, %s after",
				spelling,
				normalized,
				persisted,
			)
		}
	}
}

func manifestWithJavaDistribution(distribution string) *documentpkg.Manifest {
	return &documentpkg.Manifest{
		SchemaVersion: documentpkg.ManifestSchemaVersion,
		Server: documentpkg.ManifestServer{
			Minecraft: versionpkg.Version("1.21.4"),
			Directory: ".",
			Core: documentpkg.ManifestCore{
				Product:        serverpkg.ProductFabric,
				ProductVersion: versionpkg.Version("0.16.9"),
			},
			Java: &documentpkg.ManifestJava{
				Version:      21,
				Distribution: javapkg.Distribution(distribution),
			},
		},
	}
}
