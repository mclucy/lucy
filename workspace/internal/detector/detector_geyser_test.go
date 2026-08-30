package detector

import "testing"

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

	evidence, err := (&geyserStandaloneDetector{}).Detect(DetectionContext{}, NewDetectionFile(jarPath))
	if err != nil {
		t.Fatalf("detect standalone geyser runtime: %v", err)
	}

	return evidence
}
