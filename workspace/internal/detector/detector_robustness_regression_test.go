package detector

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestVanillaDetectorDetectsVanillaServerJson(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"version.json": `{"id":"1.21.4"}`,
	}
	jarPath := writeRootJar(t, "vanilla-server-1.21.4.jar", files)

	detector := &VanillaDetector{}

	evidence, err := detector.Detect(DetectionContext{}, NewDetectionFile(jarPath))
	if err != nil {
		t.Fatalf("detect vanilla: %v", err)
	}
	if evidence == nil {
		t.Fatalf("expected vanilla evidence, got nil")
	}
	if got := runtimeIdentityVersion(
		evidence,
		types.EcoMinecraft,
		"minecraft",
	); got != "1.21.4" {
		t.Fatalf("expected game version 1.21.4, got %q", got)
	}
	if evidence.PrimaryRuntime == nil ||
		evidence.PrimaryRuntime.Name != "minecraft" {
		t.Fatalf(
			"expected primary minecraft runtime, got %+v",
			evidence.PrimaryRuntime,
		)
	}
}

func TestVanillaDetectorRejectsForgeInstallerJson(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"version.json": `{"_comment":["one"],"mainClass":"","id":"forge-installer"}`,
	}
	jarPath := writeRootJar(t, "forge-installer.jar", files)

	detector := &VanillaDetector{}

	evidence, err := detector.Detect(DetectionContext{}, NewDetectionFile(jarPath))
	if err != nil {
		t.Fatalf("detect forge installer: %v", err)
	}
	if evidence != nil {
		t.Fatalf("expected nil evidence for forge installer, got %+v", evidence)
	}
}

func TestVanillaDetectorRejectsForgeInstallerWithEmptyComment(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"version.json": `{"_comment":[],"mainClass":"net.minecraftforge.installer","id":"forge"}`,
	}
	jarPath := writeRootJar(t, "forge-installer-empty-comment.jar", files)

	detector := &VanillaDetector{}

	evidence, err := detector.Detect(DetectionContext{}, NewDetectionFile(jarPath))
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if evidence != nil {
		t.Fatalf(
			"expected nil evidence for forge installer with mainClass set, got %+v",
			evidence,
		)
	}
}

func TestGeyserVersionExtractionFromFilename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		filename string
		want     types.BareVersion
	}{
		{"Geyser-Standalone-2.10.1-SNAPSHOT.jar", "2.10.1-snapshot"},
		{"geyser-standalone-2.4.2-b123.jar", "2.4.2-b123"},
		{"Geyser-Standalone-1.0.0.jar", "1.0.0"},
		{"geyser-standalone-2.10.1-beta.jar", "2.10.1-beta"},
		{"random.jar", types.VersionUnknown},
		{"geyser-standalone.jar", types.VersionUnknown},
	}

	for _, tt := range tests {
		t.Run(
			tt.filename, func(t *testing.T) {
				t.Parallel()

				got := parseGeyserStandaloneVersionFromPath(tt.filename)
				if got != tt.want {
					t.Fatalf(
						"parseGeyserStandaloneVersionFromPath(%q) = %q, want %q",
						tt.filename,
						got,
						tt.want,
					)
				}
			},
		)
	}
}

func TestCompareForgeMajorNumeric(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version string
		target  int
		want    int
	}{
		{"8.0.0", 61, -1},
		{"61.0.0", 61, 0},
		{"61.0", 61, 0},
		{"72.1.0", 61, 1},
		{"59.0.0", 61, -1},
		{"100.0.0", 61, 1},
		{"", 61, -1},
		{"not-a-number", 61, -1},
	}

	for _, tt := range tests {
		t.Run(
			string(tt.version), func(t *testing.T) {
				t.Parallel()

				got := compareForgeMajor(
					types.BareVersion(tt.version),
					tt.target,
				)
				sign := func(n int) int {
					if n < 0 {
						return -1
					}
					if n > 0 {
						return 1
					}
					return 0
				}(got)
				if sign != tt.want {
					t.Fatalf(
						"compareForgeMajor(%q, %d) sign = %d, want %d",
						tt.version,
						tt.target,
						sign,
						tt.want,
					)
				}
			},
		)
	}
}

func TestNeoForgeVersionRegexAllowsBetaSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bool
	}{
		{"20.4.167", true},
		{"20.4.167-beta", true},
		{"21.1.77", true},
		{"21.0.0-rc1", true},
		{"20.4", true},
		{"20.4-beta", true},
		{"20.4.167.1", true},
		{"20.4.167.1-alpha.2", true},
		{"not-a-version", false},
		{"20.4-beta-1", true},
	}

	for _, tt := range tests {
		t.Run(
			tt.input, func(t *testing.T) {
				t.Parallel()

				gameVersion, _, ok := parseModLoaderVersionTuple(
					filepath.Join("fake", "neoforge", tt.input),
					types.EcoNeoforge,
				)
				if ok != tt.want {
					t.Fatalf(
						"parseModLoaderVersionTuple(%q) ok = %v, want %v (gameVersion=%q)",
						tt.input,
						ok,
						tt.want,
						gameVersion,
					)
				}
			},
		)
	}
}

func TestNeoForgeUniversalManifestFmlSystemModsCheck(t *testing.T) {
	t.Parallel()

	manifest := `Manifest-Version: 1.0
FML-System-Mods: neoforge
Built-By: NeoForge
`
	files := map[string]string{
		"META-INF/MANIFEST.MF": manifest,
	}
	jarPath := writeRootJar(t, "neoforge-universal.jar", files)
	reader := openZipForTest(t, jarPath)
	ok, err := verifyNeoForgeUniversalManifest(reader, "21.1.77")
	if err != nil {
		t.Fatalf("verify neoforge manifest: %v", err)
	}
	if !ok {
		t.Fatalf("expected FML-System-Mods check to succeed")
	}
}

func TestParseForgeManifestOrderIndependent(t *testing.T) {
	t.Parallel()

	versionFirstManifest := `Manifest-Version: 1.0
Implementation-Version: 47.3.22
Specification-Version: 1.20.1
Implementation-Title: net.minecraftforge
`
	files := map[string]string{
		"META-INF/MANIFEST.MF": versionFirstManifest,
	}
	jarPath := writeRootJar(t, "forge-universal.jar", files)
	reader := openZipForTest(t, jarPath)
	forgeVersion, gameVersion := parseForgeManifest(reader)

	if forgeVersion != "47.3.22" {
		t.Fatalf("expected forge version 47.3.22, got %q", forgeVersion)
	}
	if gameVersion != "1.20.1" {
		t.Fatalf("expected game version 1.20.1, got %q", gameVersion)
	}
}

func TestExecutableCandidatesResolvesVanillaVsSpecific(t *testing.T) {
	t.Parallel()

	vanillaCore := types.VersionedPackageRef{
		Eco:     types.EcoMinecraft,
		Name:    "minecraft",
		Version: "1.21.4",
	}
	paperCore := types.VersionedPackageRef{
		Eco:     types.EcoPaper,
		Name:    "paper",
		Version: "1.21.4",
	}
	vanillaEvidence := &ExecutableEvidence{
		PrimaryPath:       "/fake/vanilla.jar",
		PrimaryRuntime:    &vanillaCore,
		RuntimeComponents: []types.VersionedPackageRef{vanillaCore},
	}
	paperEvidence := &ExecutableEvidence{
		PrimaryPath:    "/fake/paper.jar",
		PrimaryRuntime: &paperCore,
	}

	t.Run(
		"ambiguous_without_resolution", func(t *testing.T) {
			t.Parallel()

			candidates := &ExecutableCandidates{
				Candidates: []*ExecutableEvidence{
					vanillaEvidence, paperEvidence,
				},
			}
			if candidates.IsAmbiguous() {
				t.Fatalf("expected resolved (non-ambiguous) after dropping vanilla")
			}
			single := candidates.Single()
			if single == nil {
				t.Fatalf("expected single specific candidate, got nil")
			}
			if single.PrimaryRuntime == nil ||
				single.PrimaryRuntime.Name != "paper" {
				t.Fatalf(
					"expected paper to win, got %+v",
					single.PrimaryRuntime,
				)
			}
		},
	)

	t.Run(
		"all_vanilla_still_single", func(t *testing.T) {
			t.Parallel()

			candidates := &ExecutableCandidates{
				Candidates: []*ExecutableEvidence{vanillaEvidence},
			}
			single := candidates.Single()
			if single == nil {
				t.Fatalf("expected vanilla candidate when only vanilla exists")
			}
		},
	)

	t.Run(
		"two_specific_still_ambiguous", func(t *testing.T) {
			t.Parallel()

			forgeEvidence := &ExecutableEvidence{PrimaryPath: "/fake/forge.jar"}
			candidates := &ExecutableCandidates{
				Candidates: []*ExecutableEvidence{paperEvidence, forgeEvidence},
			}
			if !candidates.IsAmbiguous() {
				t.Fatalf("expected ambiguous when two specific candidates exist")
			}
		},
	)
}

const emptyClassBytes = "\xCA\xFE\xBA\xBE\x00\x00\x00\x34"

func openZipForTest(t *testing.T, path string) *zip.Reader {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { _ = file.Close() })

	stat, err := file.Stat()
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}

	reader, err := zip.NewReader(file, stat.Size())
	if err != nil {
		t.Fatalf("zip reader %s: %v", path, err)
	}
	return reader
}
