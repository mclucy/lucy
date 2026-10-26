package document_test

import (
	"bytes"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mclucy/lucy/catalog"
	documentpkg "github.com/mclucy/lucy/document"
	serverpkg "github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
	"gopkg.in/yaml.v3"
)

// printable keeps fuzz text inside the subset YAML represents unchanged.
func printable(value string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return ' '
		case r < 0x20, r >= 0x7f && r <= 0x9f:
			return -1
		case r == 0x2028, r == 0x2029, r == 0xfeff:
			return -1
		default:
			return r
		}
	}, strings.ToValidUTF8(value, ""))
}

// relativeDirectory is the platform-local confinement rule for server and MCDR
// directories. Manifest writes default an empty directory to "." first.
func relativeDirectory(value string) (string, bool) {
	if value == "" || filepath.IsAbs(value) {
		return "", false
	}
	cleaned := filepath.Clean(value)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(cleaned), true
}

var (
	reservedDirectory = regexp.MustCompile(`(?i)(^|/)(\.\.|\.(git|lucy))(/|$)`)
	reservedFilename  = regexp.MustCompile(`(?i)^lucy(-lock)?\.yaml(/|$)`)
)

// portablePath models a file path grammar, not the normalizer's parsing steps.
func portablePath(value string) (string, bool) {
	cleaned := path.Clean(value)
	safe := cleaned != "." && fs.ValidPath(cleaned) && filepath.IsLocal(value) &&
		!strings.ContainsAny(value, "\\:\x00") &&
		!reservedDirectory.MatchString(value) && !reservedFilename.MatchString(cleaned)
	return cleaned, safe
}

func manifestDirectory(value string) (string, bool) {
	if value == "" {
		value = "."
	}
	return relativeDirectory(value)
}

func manifestDocument(directory, tracked, selector, filePattern string, enabled *bool) *documentpkg.Manifest {
	return &documentpkg.Manifest{
		SchemaVersion: documentpkg.ManifestSchemaVersion,
		Server: documentpkg.ManifestServer{
			Minecraft: "1.21.4",
			Directory: directory,
			Core: documentpkg.ManifestCore{
				Product:        serverpkg.ProductPaper,
				ProductVersion: "1.21.4",
			},
			Packages: map[catalog.Ecosystem]map[string]documentpkg.Requirement{
				catalog.EcosystemFabric: {
					"modrinth:sodium": {
						Selector:    versionpkg.Version(selector),
						FilePattern: filePattern,
						Enabled:     enabled,
					},
				},
			},
		},
		Files: []documentpkg.TrackedFile{{Path: tracked}},
	}
}

// FuzzManifestPersistence checks the persistence boundary for one declaration:
// unsafe directories and tracked paths are never published (and do not decode),
// while accepted intent survives Write/Read and fingerprints distinguish only
// semantic changes.
func FuzzManifestPersistence(f *testing.F) {
	f.Add("", "mods/example.jar", "", "", uint8(0))
	f.Add("server", "../escape.jar", ">=1.0", "", uint8(1))
	f.Add("./server/", "/absolute.jar", "any", "*.jar", uint8(2))
	f.Add("..", ".lucy/state.json", "", "", uint8(0))
	f.Add("server", `mods\windows.jar`, " ", " ", uint8(1))
	f.Add("server", "config/server.properties", "1.2.3", "config/*", uint8(2))
	f.Add("server", "lucy.yaml", "", "", uint8(0))
	f.Add("server", "mods/a:b.jar", "", "", uint8(0))
	f.Add("server", "mods/../escape.jar", "", "", uint8(0))
	f.Add("server", ".git/config", "", "", uint8(0))
	f.Add("server", ".LUCY/config.yaml", "", "", uint8(0))
	f.Add("server", "mods/\x00.jar", "", "", uint8(0))
	f.Add(".", "NUL", "1.0.0", "", uint8(0))
	f.Add(".", "COM1.txt", "1.0.0", "", uint8(0))

	base := f.TempDir()
	f.Fuzz(func(t *testing.T, directory, tracked, selector, filePattern string, enabledMode uint8) {
		directory = printable(directory)
		tracked = strings.ToValidUTF8(tracked, "")
		selector = printable(selector)
		filePattern = printable(filePattern)

		var enabled *bool
		switch enabledMode % 3 {
		case 1:
			enabled = new(true)
		case 2:
			enabled = new(false)
		}
		document := manifestDocument(directory, tracked, selector, filePattern, enabled)

		wantDirectory, directoryOK := manifestDirectory(directory)
		wantPath, pathOK := portablePath(tracked)

		root, err := os.MkdirTemp(base, "case-")
		if err != nil {
			t.Fatalf("create case directory: %v", err)
		}
		defer func() { _ = os.RemoveAll(root) }()

		if !directoryOK || !pathOK {
			if err := documentpkg.ManifestFile(root).Write(document); err == nil {
				t.Fatalf("wrote unsafe declaration: directory=%q tracked=%q", directory, tracked)
			}
			if _, err := os.Stat(filepath.Join(root, documentpkg.ManifestFilename)); !os.IsNotExist(err) {
				t.Fatalf("unsafe declaration left a manifest: %v", err)
			}
			raw, err := yaml.Marshal(document)
			if err != nil {
				t.Fatalf("marshal unsafe declaration: %v", err)
			}
			if _, err := documentpkg.DecodeManifest(bytes.NewReader(raw)); err == nil {
				t.Fatalf("decoded unsafe declaration: directory=%q tracked=%q", directory, tracked)
			}
			return
		}

		if err := documentpkg.ManifestFile(root).Write(document); err != nil {
			t.Fatalf("write accepted declaration: %v", err)
		}
		decoded, err := documentpkg.ManifestFile(root).Read()
		if err != nil {
			t.Fatalf("read accepted declaration: %v", err)
		}
		if decoded.Server.Directory != wantDirectory {
			t.Fatalf("directory = %q, want %q", decoded.Server.Directory, wantDirectory)
		}
		requirement, ok := decoded.Server.Packages[catalog.EcosystemFabric]["modrinth:sodium"]
		if !ok {
			t.Fatal("requirement disappeared")
		}
		wantSelector := selector
		if wantSelector == "" {
			wantSelector = string(versionpkg.Any)
		}
		if string(requirement.Selector) != wantSelector {
			t.Fatalf("selector = %q, want %q", requirement.Selector, wantSelector)
		}
		if requirement.FilePattern != filePattern {
			t.Fatalf("file pattern = %q, want %q", requirement.FilePattern, filePattern)
		}
		persistedEnabled := enabledMode%3 != 2
		if requirement.IsEnabled() != persistedEnabled {
			t.Fatalf("enabled = %v, want %v", requirement.IsEnabled(), persistedEnabled)
		}
		if len(decoded.Files) != 1 || decoded.Files[0].Path != wantPath {
			t.Fatalf("files = %+v, want one %q", decoded.Files, wantPath)
		}

		fingerprint := func(document *documentpkg.Manifest) string {
			t.Helper()
			value, err := document.Fingerprint()
			if err != nil {
				t.Fatalf("fingerprint declaration: %v", err)
			}
			return value
		}
		implicitTrue := fingerprint(manifestDocument(wantDirectory, wantPath, wantSelector, filePattern, nil))
		explicitTrue := fingerprint(
			manifestDocument(wantDirectory, wantPath, wantSelector, filePattern, new(true)),
		)
		if implicitTrue != explicitTrue {
			t.Fatal("absent enabled and explicit enabled true share no fingerprint")
		}
		disabled := fingerprint(manifestDocument(wantDirectory, wantPath, wantSelector, filePattern, new(false)))
		if disabled == explicitTrue {
			t.Fatal("enabled and disabled requirements share a fingerprint")
		}
		baseline, selectorEnabled := explicitTrue, new(true)
		if !persistedEnabled {
			baseline, selectorEnabled = disabled, new(false)
		}
		changedSelector := fingerprint(
			manifestDocument(wantDirectory, wantPath, wantSelector+".changed", filePattern, selectorEnabled),
		)
		if changedSelector == baseline {
			t.Fatal("different selectors share a fingerprint")
		}
	})
}
