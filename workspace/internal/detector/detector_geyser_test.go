package detector

import (
	"archive/zip"
	"os"
	"testing"
)

func TestGeyserStandaloneDetectorDetectsStandaloneBootstrapJar(t *testing.T) {
	t.Parallel()

	jarPath := writeRootJar(
		t,
		"Geyser-Standalone.jar",
		map[string]string{
			"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nMain-Class: org.geysermc.geyser.platform.standalone.GeyserStandaloneBootstrap\nImplementation-Version: 2.5.0\n",
			"org/geysermc/geyser/platform/standalone/GeyserStandaloneBootstrap.class": "bytecode",
		},
	)

	runtime := detectGeyserStandaloneRuntimeWith(t, jarPath)
	if runtime == nil {
		t.Fatal("expected standalone Geyser detector to detect runtime")
	}
	if runtime.PrimaryRuntime == nil ||
		runtime.PrimaryRuntime.Name.String() != "geyser" {
		t.Fatalf(
			"expected primary geyser runtime, got %+v",
			runtime.PrimaryRuntime,
		)
	}
}

func detectGeyserStandaloneRuntimeWith(t *testing.T, jarPath string) *ExecutableEvidence {
	t.Helper()

	file, err := os.Open(jarPath)
	if err != nil {
		t.Fatalf("open jar: %v", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		t.Fatalf("stat jar: %v", err)
	}

	reader, err := zip.NewReader(file, stat.Size())
	if err != nil {
		t.Fatalf("read zip: %v", err)
	}

	evidence, err := (&geyserStandaloneDetector{}).Detect(jarPath, reader, file)
	if err != nil {
		t.Fatalf("detect standalone geyser runtime: %v", err)
	}

	return evidence
}
