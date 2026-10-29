package discovery

import (
	"path/filepath"
	"testing"
)

func TestGeyserDetectorFindsStandaloneJar(t *testing.T) {
	t.Parallel()

	jarPath := writeRootJar(
		t,
		"Geyser-Standalone.jar",
		map[string]string{
			"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nMain-Class: org.geysermc.geyser.platform.standalone.GeyserStandaloneBootstrap\nImplementation-Version: 2.5.0\n",
			"org/geysermc/geyser/platform/standalone/GeyserStandaloneBootstrap.class": "bytecode",
		},
	)

	installation := detectGeyserStandaloneRuntimeWith(t, jarPath)
	if installation.Identity.Primary.Name.String() != "geyser" {
		t.Fatalf(
			"expected primary geyser runtime, got %+v",
			installation.Identity.Primary,
		)
	}
}

func detectGeyserStandaloneRuntimeWith(
	t *testing.T,
	jarPath string,
) Installation {
	t.Helper()

	installations, _ := discoverGeyser(productFixtureScan(t, filepath.Dir(jarPath)))
	installation := singleProductInstallation(t, installations)
	return installation
}
