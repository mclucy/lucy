package discovery

import (
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/catalog"
)

func TestSpongeServerDetector_UsesSpecificationTitleOverImplementationTitle(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"META-INF/MANIFEST.MF": "" +
			"Manifest-Version: 1.0\n" +
			"Specification-Title: SpongeVanilla\n" +
			"Specification-Vendor: SpongePowered\n" +
			"Implementation-Title: wrong-subproject-name\n" +
			"Implementation-Vendor: SpongePowered\n" +
			"Implementation-Version: 1.21.10-17.0.0\n",
	}
	jarPath := writeRootJar(t, "spongevanilla-spec-title.jar", files)

	installations, _ := discoverSponge(productFixtureScan(t, filepath.Dir(jarPath)))
	installation := singleProductInstallation(t, installations)
	if installation.Identity.Primary.Name != "spongevanilla" ||
		identityComponentVersion(
			installation.Identity,
			catalog.EcosystemMinecraft,
			"minecraft",
		) != "1.21.10" {
		t.Fatalf(
			"expected spongevanilla from specification title, got %+v",
			installation.Identity,
		)
	}
}

func TestSpongeServerDetector_RejectsNonSpongeJar(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nImplementation-Title: NotSponge\nImplementation-Vendor: SpongePowered\nImplementation-Version: 1.21.10-17.0.0\n",
	}
	jarPath := writeRootJar(t, "random-server.jar", files)

	installations, _ := discoverSponge(productFixtureScan(t, filepath.Dir(jarPath)))
	if len(installations) != 0 {
		t.Fatalf(
			"expected no sponge installation for non-sponge jar name, got %v",
			productInstallationNames(installations),
		)
	}
}
