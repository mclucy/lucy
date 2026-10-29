package document_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mclucy/lucy/catalog"
	documentpkg "github.com/mclucy/lucy/document"
	javapkg "github.com/mclucy/lucy/java"
	"github.com/mclucy/lucy/javaruntime"
	serverpkg "github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
	"gopkg.in/yaml.v3"
)

var acceptedDigest = strings.Repeat("0123456789abcdef", 4)

// lockRelativeDirectory is the platform-local confinement rule applied to
// lock directories. Unlike the manifest, a lock requires an explicit directory.
func lockRelativeDirectory(value string) bool {
	if value == "" || filepath.IsAbs(value) {
		return false
	}
	cleaned := filepath.Clean(value)
	return cleaned != ".." && !strings.HasPrefix(cleaned, ".."+string(filepath.Separator))
}

func lockDocument(directory, tracked, pinVersion, selector string, enabled bool) *documentpkg.Lock {
	return &documentpkg.Lock{
		SchemaVersion:       documentpkg.LockSchemaVersion,
		ManifestFingerprint: "sha256:manifest",
		Server: documentpkg.LockedServer{
			Directory:   directory,
			Minecraft:   "1.21.4",
			Product:     serverpkg.ProductPaper,
			CoreVersion: "1.21.4",
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderPaperMC,
				ProjectID: "paper",
				Version:   "1.21.4",
				Filename:  "paper.jar",
				Hashes:    map[string]string{"sha256": acceptedDigest},
			},
			Java: documentpkg.LockedJava{Advisory: 21},
		},
		Packages: []documentpkg.Package{{
			Environment: serverpkg.EnvironmentServer,
			Ecosystem:   catalog.EcosystemFabric,
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderModrinth,
				ProjectID: "sodium",
				Version:   versionpkg.Version(pinVersion),
				Filename:  "sodium.jar",
				Hashes:    map[string]string{"sha256": acceptedDigest},
			},
			Requirements: []documentpkg.RequirementBinding{{
				Reference: "modrinth:sodium",
				Selector:  versionpkg.Version(selector),
				Enabled:   enabled,
			}},
		}},
		Files: []documentpkg.TrackedFile{{Path: tracked}},
	}
}

// FuzzLockReplayPersistence checks that unsafe replay paths are neither
// published nor decoded, and that an accepted lock preserves the pinned
// artifact and requirement binding. Fingerprints must distinguish an enabled
// binding from a disabled one and locks bound to different manifests.
func FuzzLockReplayPersistence(f *testing.F) {
	f.Add(".", "config/server.properties", "1.0.0", ">=1.0", true, false)
	f.Add(".", "config/server.properties", "1.0.0", ">=1.0", false, true)
	f.Add("../outside", "../escape.jar", "1.0.0", ">=1.0", false, false)
	f.Add("", "mods/a.jar", "1.0.0", ">=1.0", true, false)
	f.Add("server", ".lucy/install.yaml", "any", "", false, false)
	f.Add("./server/", "mods//a.jar", "1.0.0", "any", true, false)
	f.Add("server", "lucy-lock.yaml", "none", ">=2", true, false)
	f.Add("server", "mods/../escape.jar", "1.0.0", "any", true, false)
	f.Add("server", ".git/config", "1.0.0", "any", false, false)
	f.Add("server", "LUCY.YAML/runtime", "1.0.0", "any", false, false)
	f.Add("server", "mods/\x00.jar", "1.0.0", "any", true, false)
	f.Add(".", "NUL", "1.0.0", "any", true, false)
	f.Add(".", "COM1.txt", "1.0.0", "any", true, false)

	base := f.TempDir()
	f.Fuzz(func(t *testing.T, directory, tracked, pinVersion, selector string, enabled, dependency bool) {
		directory = printable(directory)
		tracked = strings.ToValidUTF8(tracked, "")
		pinVersion = printable(pinVersion)
		selector = printable(selector)

		document := lockDocument(directory, tracked, pinVersion, selector, enabled)
		document.Packages[0].Dependency = dependency
		wantPath, pathOK := portablePath(tracked)
		pin := versionpkg.Version(pinVersion)
		concretePin := pinVersion != "" && !pin.CanInfer() && !pin.IsInvalid()
		wantAccept := lockRelativeDirectory(directory) && pathOK && concretePin && selector != ""

		root, err := os.MkdirTemp(base, "case-")
		if err != nil {
			t.Fatalf("create case directory: %v", err)
		}
		defer func() { _ = os.RemoveAll(root) }()

		if !wantAccept {
			if err := documentpkg.LockFile(root).Write(document); err == nil {
				t.Fatalf("wrote unsafe lock: directory=%q tracked=%q pin=%q selector=%q", directory, tracked, pinVersion, selector)
			}
			if _, err := os.Stat(filepath.Join(root, documentpkg.LockFilename)); !os.IsNotExist(err) {
				t.Fatalf("unsafe lock left a file: %v", err)
			}
			raw, err := yaml.Marshal(document)
			if err != nil {
				t.Fatalf("marshal unsafe lock: %v", err)
			}
			if _, err := documentpkg.DecodeLock(bytes.NewReader(raw)); err == nil {
				t.Fatalf("decoded unsafe lock: directory=%q tracked=%q pin=%q selector=%q", directory, tracked, pinVersion, selector)
			}
			return
		}

		if err := documentpkg.LockFile(root).Write(document); err != nil {
			t.Fatalf("write accepted lock: %v", err)
		}
		decoded, err := documentpkg.LockFile(root).Read()
		if err != nil {
			t.Fatalf("read accepted lock: %v", err)
		}
		if decoded.ManifestFingerprint != "sha256:manifest" {
			t.Fatalf("manifest fingerprint = %q", decoded.ManifestFingerprint)
		}
		if decoded.Server.Directory != directory {
			t.Fatalf("directory = %q, want %q", decoded.Server.Directory, directory)
		}
		if len(decoded.Packages) != 1 || len(decoded.Packages[0].Requirements) != 1 {
			t.Fatalf("packages = %+v, want one package with one binding", decoded.Packages)
		}
		pkg := decoded.Packages[0]
		if pkg.Artifact.Provider != catalog.ProviderModrinth ||
			pkg.Artifact.ProjectID != "sodium" ||
			pkg.Artifact.Version != pin ||
			pkg.Artifact.Filename != "sodium.jar" ||
			pkg.Artifact.Hashes["sha256"] != acceptedDigest {
			t.Fatalf("pinned artifact = %+v", pkg.Artifact)
		}
		binding := pkg.Requirements[0]
		if binding.Reference != "modrinth:sodium" ||
			binding.Selector != versionpkg.Version(selector) ||
			binding.Enabled != enabled {
			t.Fatalf("binding = %+v", binding)
		}
		if wantEnabled := enabled || dependency; pkg.Enabled() != wantEnabled {
			t.Fatalf("package enabled = %v, want %v after replay", pkg.Enabled(), wantEnabled)
		}
		if len(decoded.Files) != 1 || decoded.Files[0].Path != wantPath {
			t.Fatalf("files = %+v, want one %q", decoded.Files, wantPath)
		}

		fingerprint := func(document *documentpkg.Lock) string {
			t.Helper()
			value, err := document.Fingerprint()
			if err != nil {
				t.Fatalf("fingerprint lock: %v", err)
			}
			return value
		}
		enabledFingerprint := fingerprint(lockDocument(directory, wantPath, pinVersion, selector, true))
		disabledFingerprint := fingerprint(lockDocument(directory, wantPath, pinVersion, selector, false))
		if enabledFingerprint == disabledFingerprint {
			t.Fatal("enabled and disabled bindings share a fingerprint")
		}
		changedIntent := lockDocument(directory, wantPath, pinVersion, selector, enabled)
		changedIntent.ManifestFingerprint += "-other"
		currentFingerprint := enabledFingerprint
		if !enabled {
			currentFingerprint = disabledFingerprint
		}
		if fingerprint(changedIntent) == currentFingerprint {
			t.Fatal("locks bound to different manifests share a fingerprint")
		}
	})
}

// FuzzLockJavaDistributionReplay protects the replay boundary for Java
// distributions: a spelling ParseDistribution accepts must decode to its
// canonical form, survive Snapshot/Decode, and accept a matching runtime
// through the same RequirementOf/Misses path installation uses.
func FuzzLockJavaDistributionReplay(f *testing.F) {
	for _, spelling := range []string{
		"", "any", "ANY", "temurin", "TEMURIN", "Temurin", " zulu ", "bogus",
	} {
		f.Add(spelling)
	}
	f.Fuzz(func(t *testing.T, spelling string) {
		spelling = printable(spelling)
		canonical, supported := javapkg.ParseDistribution(spelling)

		document := lockDocument(".", "config/server.properties", "1.0.0", ">=1.0", true)
		document.Server.Java.Distribution = javapkg.Distribution(spelling)
		raw, err := yaml.Marshal(document)
		if err != nil {
			t.Fatalf("marshal lock with distribution %q: %v", spelling, err)
		}
		decoded, err := documentpkg.DecodeLock(bytes.NewReader(raw))
		if !supported {
			if err == nil {
				t.Fatalf("lock accepted unknown Java distribution %q", spelling)
			}
			return
		}
		if err != nil {
			t.Fatalf("lock rejected supported Java distribution %q: %v", spelling, err)
		}
		if decoded.Server.Java.Distribution != canonical {
			t.Fatalf(
				"decoded distribution = %q, want canonical %q for %q",
				decoded.Server.Java.Distribution, canonical, spelling,
			)
		}

		snapshot, err := decoded.Snapshot()
		if err != nil {
			t.Fatalf("snapshot decoded lock: %v", err)
		}
		replayed, err := documentpkg.DecodeLock(bytes.NewReader(snapshot))
		if err != nil {
			t.Fatalf("decode lock snapshot: %v", err)
		}
		if replayed.Server.Java.Distribution != canonical {
			t.Fatalf(
				"snapshot distribution = %q, want canonical %q for %q",
				replayed.Server.Java.Distribution, canonical, spelling,
			)
		}

		runtimeDistribution := canonical
		if runtimeDistribution == javapkg.DistributionAny {
			runtimeDistribution = javapkg.DistributionTemurin
		}
		misses := javaruntime.RequirementOf(replayed).Misses(&javaruntime.Runtime{
			Home:         "/opt/java",
			Version:      "21.0.1",
			Major:        21,
			Distribution: runtimeDistribution,
		})
		if len(misses) != 0 {
			t.Fatalf(
				"lock persisted distribution %q but rejects its canonical %q runtime: %v",
				spelling, runtimeDistribution, misses,
			)
		}
	})
}
