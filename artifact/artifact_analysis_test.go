package artifact

import (
	"os"
	"testing"

	"github.com/mclucy/lucy/catalog"
)

func TestAnalysisRejectsEvidenceAfterReplacement(t *testing.T) {
	t.Parallel()
	path := writeArtifactJar(t, map[string]string{"META-INF/mods.toml": forgeModsToml})
	var analyses Analyses
	analysis := analyses.File(path)
	readers := map[string]func() error{
		"inventory": func() error {
			_, err := analysis.Info()
			return err
		},
		"descriptor": func() error {
			_, err := analysis.Descriptor(t.Context(), catalog.EcosystemForge, "1.21.1")
			return err
		},
		"sha1": func() error {
			_, err := analysis.SHA1()
			return err
		},
		"sha256": func() error {
			_, err := analysis.SHA256()
			return err
		},
		"murmur": func() error {
			_, err := analysis.MurmurHash()
			return err
		},
	}
	for name, read := range readers {
		if err := read(); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
	}
	replacement := writeArtifactJar(t, map[string]string{"META-INF/mods.toml": neoforgeModsToml})
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	for name, read := range readers {
		if err := read(); err == nil {
			t.Errorf("%s accepted cached evidence for a replaced artifact", name)
		}
	}
}
