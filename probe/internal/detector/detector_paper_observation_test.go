package detector

import (
	"archive/zip"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestPaperObservationCollector_AnchorsFixtureRoot(t *testing.T) {
	t.Parallel()

	root := paperFamilyFixtureRoot(t)
	if filepath.Base(root) != "paper_family" {
		t.Fatalf("fixture root mismatch: %s", root)
	}

	checks := []string{
		paperFamilyFixturePath(
			t,
			"test_paper",
			"paper",
			"META-INF",
			"libraries.list",
		),
		paperFamilyFixturePath(
			t,
			"test_folia",
			"folia",
			"META-INF",
			"libraries.list",
		),
		paperFamilyFixturePath(
			t,
			"test_leaf",
			"leaf",
			"META-INF",
			"MANIFEST.MF",
		),
		paperFamilyFixturePath(
			t,
			"test_leaves",
			"leaves",
			"META-INF",
			"build-info",
		),
		paperFamilyFixturePath(t, "test_reaper", "reaper", "patch.properties"),
		paperFamilyFixturePath(
			t,
			"test_youer",
			"youer",
			"META-INF",
			"MANIFEST.MF",
		),
	}

	for _, path := range checks {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected anchored fixture path %s: %v", path, err)
		}
	}
}

func TestPaperObservationCollector_PaperFixtureSupportsTreeAndJar(t *testing.T) {
	t.Parallel()

	fixtureRoot := paperFamilyFixturePath(t, "test_paper", "paper")

	treeObs, err := extractPaperObservations(fixtureRoot, nil)
	if err != nil {
		t.Fatalf("extract observations from tree: %v", err)
	}
	assertPaperFixtureObservations(t, treeObs)

	jarPath := writeFixtureJar(t, fixtureRoot, "paper-fixture.jar")
	zipReader := openTestZipReader(t, jarPath)

	jarObs, err := extractPaperObservations(jarPath, zipReader)
	if err != nil {
		t.Fatalf("extract observations from jar: %v", err)
	}
	assertPaperFixtureObservations(t, jarObs)

	if treeObs.metaMainClass != jarObs.metaMainClass {
		t.Fatalf(
			"META-INF/main-class mismatch: tree=%q jar=%q",
			treeObs.metaMainClass,
			jarObs.metaMainClass,
		)
	}
	if treeObs.manifestMainClass != jarObs.manifestMainClass {
		t.Fatalf(
			"manifest main class mismatch: tree=%q jar=%q",
			treeObs.manifestMainClass,
			jarObs.manifestMainClass,
		)
	}
	if treeObs.gameVersion != jarObs.gameVersion {
		t.Fatalf(
			"game version mismatch: tree=%q jar=%q",
			treeObs.gameVersion,
			jarObs.gameVersion,
		)
	}
	if len(treeObs.librariesListEntries) != len(jarObs.librariesListEntries) {
		t.Fatalf(
			"libraries.list entry count mismatch: tree=%d jar=%d",
			len(treeObs.librariesListEntries),
			len(jarObs.librariesListEntries),
		)
	}
}

func TestPaperObservationCollector_ForkFixturesExposeRawEvidence(t *testing.T) {
	t.Parallel()

	t.Run(
		"folia libraries marker", func(t *testing.T) {
			t.Parallel()

			obs := extractFixtureObservations(t, "test_folia", "folia")
			if !containsObservationToken(
				obs.librariesListEntries,
				"dev.folia:folia-api:1.21.11-R0.1-SNAPSHOT",
			) {
				t.Fatalf("expected folia api marker in libraries.list")
			}
			if obs.manifestMainClass != "io.papermc.paperclip.Main" {
				t.Fatalf(
					"unexpected folia manifest main class: %q",
					obs.manifestMainClass,
				)
			}
			if !obs.hasPaperclipNamespace {
				t.Fatalf("expected paperclip namespace marker for folia fixture")
			}
		},
	)

	t.Run(
		"leaves sidecar evidence", func(t *testing.T) {
			t.Parallel()

			obs := extractFixtureObservations(t, "test_leaves", "leaves")
			if obs.buildInfo != "Leaves\t1.21.8\t138" {
				t.Fatalf("unexpected build-info: %q", obs.buildInfo)
			}
			if obs.leavesclipVersion != "3.0.7" {
				t.Fatalf(
					"unexpected leavesclip version: %q",
					obs.leavesclipVersion,
				)
			}
			if !containsObservationToken(
				obs.librariesListEntries,
				"org.leavesmc.leaves:leaves-api:1.21.8-R0.1-SNAPSHOT",
			) {
				t.Fatalf("expected leaves api marker in libraries.list")
			}
			if !obs.hasLeavesclipNamespace {
				t.Fatalf("expected leavesclip namespace marker")
			}
		},
	)

	t.Run(
		"reaper patch properties", func(t *testing.T) {
			t.Parallel()

			obs := extractFixtureObservations(t, "test_reaper", "reaper")
			if obs.patchProperties["patch"] != "paperMC.patch" {
				t.Fatalf("unexpected patch property: %#v", obs.patchProperties)
			}
			if !obs.hasPaperMCPatch {
				t.Fatalf("expected paperMC.patch presence")
			}
			if obs.manifestMainClass != "io.papermc.paperclip.Paperclip" {
				t.Fatalf(
					"unexpected reaper manifest main class: %q",
					obs.manifestMainClass,
				)
			}
			if !obs.hasPaperclipNamespace {
				t.Fatalf("expected paperclip namespace marker for reaper fixture")
			}
			if obs.gameVersion != types.BareVersion("1.12.2") {
				t.Fatalf("unexpected reaper game version: %q", obs.gameVersion)
			}
		},
	)

	t.Run(
		"youer manifest heavy evidence", func(t *testing.T) {
			t.Parallel()

			obs := extractFixtureObservations(t, "test_youer", "youer")
			if obs.manifestMainClass != "com.mohistmc.launcher.youer.Main" {
				t.Fatalf(
					"unexpected youer manifest main class: %q",
					obs.manifestMainClass,
				)
			}
			if obs.manifestSpecificationTitle != "Youer" {
				t.Fatalf(
					"unexpected youer specification title: %q",
					obs.manifestSpecificationTitle,
				)
			}
			if obs.manifestImplementationTitle != "Youer" {
				t.Fatalf(
					"unexpected youer implementation title: %q",
					obs.manifestImplementationTitle,
				)
			}
			if obs.manifestSpecificationVendor != "MohistMC" {
				t.Fatalf(
					"unexpected youer specification vendor: %q",
					obs.manifestSpecificationVendor,
				)
			}
			if obs.manifestImplementationVendor != "MohistMC" {
				t.Fatalf(
					"unexpected youer implementation vendor: %q",
					obs.manifestImplementationVendor,
				)
			}
			if !obs.hasYouerNamespace {
				t.Fatalf("expected youer namespace marker")
			}
			if obs.gameVersion != types.BareVersion("1.21.1") {
				t.Fatalf("unexpected youer game version: %q", obs.gameVersion)
			}
		},
	)
}

func TestPaperObservationCollector_MissingOptionalFilesGracefullyDegrades(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestDir := filepath.Join(dir, "META-INF")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatalf("mkdir META-INF: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(manifestDir, "MANIFEST.MF"),
		[]byte("Manifest-Version: 1.0\nMain-Class: io.papermc.paperclip.Main\n\n"),
		0o644,
	); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	obs, err := extractPaperObservations(dir, nil)
	if err != nil {
		t.Fatalf("extract minimal observations: %v", err)
	}

	if obs.metaMainClass != "" {
		t.Fatalf(
			"expected empty META-INF/main-class, got %q",
			obs.metaMainClass,
		)
	}
	if len(obs.librariesListEntries) != 0 {
		t.Fatalf(
			"expected empty libraries.list entries, got %d",
			len(obs.librariesListEntries),
		)
	}
	if len(obs.versionsListEntries) != 0 {
		t.Fatalf(
			"expected empty versions.list entries, got %d",
			len(obs.versionsListEntries),
		)
	}
	if len(obs.patchesListEntries) != 0 {
		t.Fatalf(
			"expected empty patches.list entries, got %d",
			len(obs.patchesListEntries),
		)
	}
	if obs.downloadContext != "" {
		t.Fatalf("expected empty download-context, got %q", obs.downloadContext)
	}
	if len(obs.patchProperties) != 0 {
		t.Fatalf(
			"expected empty patch properties, got %#v",
			obs.patchProperties,
		)
	}
	if obs.buildInfo != "" {
		t.Fatalf("expected empty build-info, got %q", obs.buildInfo)
	}
	if obs.leavesclipVersion != "" {
		t.Fatalf(
			"expected empty leavesclip-version, got %q",
			obs.leavesclipVersion,
		)
	}
	if obs.hasPaperMCPatch {
		t.Fatalf("expected paperMC.patch to be absent")
	}
	if obs.manifestMainClass != "io.papermc.paperclip.Main" {
		t.Fatalf("unexpected manifest main class: %q", obs.manifestMainClass)
	}
	if obs.gameVersion != types.VersionUnknown {
		t.Fatalf("expected unknown game version, got %q", obs.gameVersion)
	}
}

func assertPaperFixtureObservations(t *testing.T, obs paperObservations) {
	t.Helper()

	if obs.metaMainClass != "org.bukkit.craftbukkit.Main" {
		t.Fatalf("unexpected META-INF/main-class: %q", obs.metaMainClass)
	}
	if obs.manifestMainClass != "io.papermc.paperclip.Main" {
		t.Fatalf("unexpected manifest main class: %q", obs.manifestMainClass)
	}
	if !containsObservationToken(
		obs.librariesListEntries,
		"io.papermc.paper:paper-api:1.21.11-R0.1-SNAPSHOT",
	) {
		t.Fatalf("expected paper api marker in libraries.list")
	}
	if !containsObservationToken(
		obs.versionsListEntries,
		"1.21.11\t1.21.11/paper-1.21.11.jar",
	) {
		t.Fatalf("expected versions.list entry")
	}
	if !containsObservationToken(
		obs.patchesListEntries,
		"1.21.11/server-1.21.11.jar.patch",
	) {
		t.Fatalf("expected patches.list server patch entry")
	}
	if obs.downloadContext == "" {
		t.Fatalf("expected download-context to be captured")
	}
	if obs.versionJSONID != types.BareVersion("1.21.11") {
		t.Fatalf("unexpected version.json id: %q", obs.versionJSONID)
	}
	if !obs.hasPaperclipNamespace {
		t.Fatalf("expected paperclip namespace marker")
	}
	if obs.gameVersion != types.BareVersion("1.21.11") {
		t.Fatalf("unexpected game version: %q", obs.gameVersion)
	}
}

func extractFixtureObservations(
	t *testing.T,
	brand string,
	parts ...string,
) paperObservations {
	t.Helper()

	obs, err := extractPaperObservations(
		paperFamilyFixturePath(
			t,
			brand,
			parts...,
		), nil,
	)
	if err != nil {
		t.Fatalf("extract observations from %s: %v", brand, err)
	}
	return obs
}

func paperFamilyFixtureRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("cannot locate test file path")
	}
	return filepath.Clean(
		filepath.Join(
			filepath.Dir(file),
			"testdata",
			"paper_family",
		),
	)
}

func paperFamilyFixturePath(
	t *testing.T,
	brand string,
	parts ...string,
) string {
	t.Helper()
	segments := append([]string{paperFamilyFixtureRoot(t), brand}, parts...)
	return filepath.Join(segments...)
}

func writeFixtureJar(t *testing.T, sourceDir string, name string) string {
	t.Helper()

	jarPath := filepath.Join(t.TempDir(), name)
	file, err := os.Create(jarPath)
	if err != nil {
		t.Fatalf("create fixture jar: %v", err)
	}
	defer file.Close()

	writer := zip.NewWriter(file)
	err = filepath.WalkDir(
		sourceDir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}

			rel, err := filepath.Rel(sourceDir, path)
			if err != nil {
				return err
			}

			entry, err := writer.Create(filepath.ToSlash(rel))
			if err != nil {
				return err
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, err = entry.Write(data)
			return err
		},
	)
	if err != nil {
		t.Fatalf("walk fixture tree: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close fixture jar writer: %v", err)
	}

	return jarPath
}

func openTestZipReader(t *testing.T, jarPath string) *zip.Reader {
	t.Helper()

	file, err := os.Open(jarPath)
	if err != nil {
		t.Fatalf("open jar: %v", err)
	}
	t.Cleanup(
		func() {
			_ = file.Close()
		},
	)

	stat, err := file.Stat()
	if err != nil {
		t.Fatalf("stat jar: %v", err)
	}

	reader, err := zip.NewReader(file, stat.Size())
	if err != nil {
		t.Fatalf("open zip reader: %v", err)
	}
	return reader
}

func containsObservationToken(lines []string, want string) bool {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}
