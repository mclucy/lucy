package manifest_test

import (
	"testing"

	javapkg "github.com/mclucy/lucy/java"
	"github.com/mclucy/lucy/manifest"
	serverpkg "github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
)

// TestJavaDistributionNormalizes covers the spelling of a declared Java
// distribution. Normalize decides what the manifest says, so two spellings of
// one distribution must normalize, persist, and fingerprint alike.
func TestJavaDistributionNormalizes(t *testing.T) {
	for _, spelling := range []string{"TEMURIN", "Temurin", " temurin "} {
		document := manifestWithJavaDistribution(spelling)
		if err := manifest.Normalize(document); err != nil {
			t.Fatalf("Normalize(%q) error = %v", spelling, err)
		}
		if got := document.Server.Java.Distribution; got != javapkg.DistributionTemurin {
			t.Fatalf("Normalize(%q) distribution = %q, want %q", spelling, got, javapkg.DistributionTemurin)
		}
		normalized, err := manifest.Fingerprint(document)
		if err != nil {
			t.Fatalf("Fingerprint(%q) error = %v", spelling, err)
		}

		root := t.TempDir()
		if err := manifest.Write(root, document); err != nil {
			t.Fatalf("Write(%q) error = %v", spelling, err)
		}
		decoded, err := manifest.Read(root)
		if err != nil {
			t.Fatalf("Read(%q) error = %v", spelling, err)
		}
		if got := decoded.Server.Java.Distribution; got != javapkg.DistributionTemurin {
			t.Fatalf("Read(%q) distribution = %q, want %q", spelling, got, javapkg.DistributionTemurin)
		}
		persisted, err := manifest.Fingerprint(decoded)
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

func manifestWithJavaDistribution(distribution string) *manifest.Document {
	return &manifest.Document{
		SchemaVersion: manifest.SchemaVersion,
		Server: manifest.Server{
			Minecraft: versionpkg.Version("1.21.4"),
			Directory: ".",
			Core: manifest.Core{
				Product:        serverpkg.ProductFabric,
				ProductVersion: versionpkg.Version("0.16.9"),
			},
			Java: &manifest.Java{
				Version:      21,
				Distribution: javapkg.Distribution(distribution),
			},
		},
	}
}
