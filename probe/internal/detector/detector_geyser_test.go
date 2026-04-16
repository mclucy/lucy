package detector

import (
	"archive/zip"
	"os"
	"testing"

	"github.com/mclucy/lucy/types"
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
	if runtime.Topology == nil {
		t.Fatal("expected topology on detected runtime")
	}
	if runtime.Topology.PrimaryNode != types.RuntimeNodeID("geyser_standalone") {
		t.Fatalf("expected standalone geyser primary node, got %q", runtime.Topology.PrimaryNode)
	}
	if got := runtime.Topology.Nodes[0].Role; got != types.RuntimeRoleProxy {
		t.Fatalf("expected standalone geyser role %q, got %q", types.RuntimeRoleProxy, got)
	}
	if len(runtime.RuntimeIdentities) == 0 || runtime.RuntimeIdentities[0].Name.String() != "geyser" {
		t.Fatalf("expected primary runtime identity for geyser, got %+v", runtime.RuntimeIdentities)
	}
}

func TestDetectBridgeMarkersPrefersStandaloneGeyserMarker(t *testing.T) {
	t.Parallel()

	jarPath := writeRootJar(
		t,
		"Geyser-Standalone.jar",
		map[string]string{
			"org/geysermc/geyser/platform/standalone/GeyserStandaloneBootstrap.class": "bytecode",
			"org/geysermc/geyser/GeyserImpl.class":                                    "bytecode",
		},
	)

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

	markers := DetectBridgeMarkers(reader)
	if len(markers) != 1 {
		t.Fatalf("expected exactly one bridge marker, got %+v", markers)
	}
	if markers[0].NodeID != "geyser_standalone" {
		t.Fatalf("expected standalone geyser marker, got %+v", markers[0])
	}
}

func detectGeyserStandaloneRuntimeWith(t *testing.T, jarPath string) *types.RuntimeInfo {
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

	runtime, err := (&geyserStandaloneDetector{}).Detect(jarPath, reader, file)
	if err != nil {
		t.Fatalf("detect standalone geyser runtime: %v", err)
	}

	return runtime
}
