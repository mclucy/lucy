package detector

import (
	"testing"

	"github.com/mclucy/lucy/types"
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
	reader := openZipForTest(t, jarPath)
	det := &spongeServerDetector{}

	evidence, err := det.Detect(jarPath, reader, nil)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if evidence == nil ||
		evidence.PrimaryRuntime == nil ||
		evidence.PrimaryRuntime.Name != "spongevanilla" ||
		runtimeIdentityVersion(
			evidence,
			types.EcoMinecraft,
			"minecraft",
		) != "1.21.10" {
		t.Fatalf(
			"expected spongevanilla from specification title, got %+v",
			evidence,
		)
	}
}

func TestSpongeServerDetector_RejectsNonSpongeJar(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nImplementation-Title: NotSponge\nImplementation-Vendor: SpongePowered\nImplementation-Version: 1.21.10-17.0.0\n",
	}
	jarPath := writeRootJar(t, "random-server.jar", files)
	reader := openZipForTest(t, jarPath)
	det := &spongeServerDetector{}

	evidence, err := det.Detect(jarPath, reader, nil)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if evidence != nil {
		t.Fatalf("expected nil for non-sponge jar name, got %+v", evidence)
	}
}
