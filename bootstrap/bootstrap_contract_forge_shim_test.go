package bootstrap_test

import (
	"archive/zip"
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
)

const (
	forgeShimTestRoot   = "forge-1.21.11-61.2.0-shim.jar"
	forgeShimTestLib    = "libraries/net/minecraftforge/forge/1.21.11-61.2.0/forge-1.21.11-61.2.0-shim.jar"
	forgeShimTestArgs   = "libraries/net/minecraftforge/forge/1.21.11-61.2.0/unix_args.txt"
	forgeShimTestLaunch = "java @user_jvm_args.txt @" + forgeShimTestArgs + " \"$@\"\n"
	forgeShimTestInvoke = "java -jar " + forgeShimTestRoot + " --onlyCheckJava || exit 1\n"
	forgeShimHeredocSh  = "#!/usr/bin/env sh\n" +
		"cat <<'EOF' > check.sh\n" +
		"java -jar " + forgeShimTestRoot + " --onlyCheckJava || exit 1\n" +
		"EOF\n" + forgeShimTestLaunch
	forgeShimConditionalSh = "#!/usr/bin/env sh\n" +
		"if [ -f " + forgeShimTestRoot + " ]; then\n" +
		"java -jar " + forgeShimTestRoot + " --onlyCheckJava || exit 1\n" +
		"fi\n" + forgeShimTestLaunch
)

// TestForgeRootShimAttribution pins which root shim a shim-era Forge tree
// claims. The root copy is admitted only when run.sh has the generated launcher
// shape and the root copy byte-matches the canonical shim under libraries, which
// must be a JAR whose manifest names the Forge shim main class. Both staging and
// existing-tree checks apply this rule. A named root shim that fails it is an
// error, never silently unclaimed.
func TestForgeRootShimAttribution(t *testing.T) {
	t.Parallel()
	const shimEraRunSh = `#!/usr/bin/env sh
# Add custom JVM arguments (such as RAM allocation) to the user_jvm_args.txt

java -jar forge-1.21.11-61.2.0-shim.jar --onlyCheckJava || exit 1

# Add custom program arguments (such as nogui) to the next line before the "$@" or pass them to this script directly
java @user_jvm_args.txt @libraries/net/minecraftforge/forge/1.21.11-61.2.0/unix_args.txt "$@"
`
	shim := shimJar(t, "A")
	other := shimJar(t, "B")
	wrong := wrongMainJar(t)

	cases := []struct {
		name            string
		runSh           string
		rootShim        []byte            // nil: no root shim file
		canonical       []byte            // canonical shim under libraries; nil: none
		extra           map[string][]byte // additional files relative to the server root
		existingStatus  bootstrap.CurrentnessStatus
		existingClaimed bool
		stageErr        bool
		stageClaimed    bool
	}{
		{
			name:            "canonical shim claims root shim",
			runSh:           shimEraRunSh,
			rootShim:        shim,
			canonical:       shim,
			existingStatus:  bootstrap.CurrentnessInconclusive,
			existingClaimed: true,
			stageClaimed:    true,
		},
		{
			name:           "root differs from canonical shim",
			runSh:          shimEraRunSh,
			rootShim:       other,
			canonical:      shim,
			existingStatus: bootstrap.CurrentnessMismatch,
			stageErr:       true,
		},
		{
			name:           "missing canonical shim rejects named root shim",
			runSh:          shimEraRunSh,
			rootShim:       shim,
			existingStatus: bootstrap.CurrentnessMismatch,
			stageErr:       true,
		},
		{
			name:           "non-JAR canonical shim rejects named root shim",
			runSh:          shimEraRunSh,
			rootShim:       []byte("not a jar"),
			canonical:      []byte("not a jar"),
			existingStatus: bootstrap.CurrentnessMismatch,
			stageErr:       true,
		},
		{
			name:           "JAR of another entry point rejects named root shim",
			runSh:          shimEraRunSh,
			rootShim:       wrong,
			canonical:      wrong,
			existingStatus: bootstrap.CurrentnessMismatch,
			stageErr:       true,
		},
		{
			name:           "invalid root copy rejects against valid canonical shim",
			runSh:          shimEraRunSh,
			rootShim:       []byte("not a jar"),
			canonical:      shim,
			existingStatus: bootstrap.CurrentnessMismatch,
			stageErr:       true,
		},
		{
			name:           "comment mention does not claim root shim",
			runSh:          "#!/usr/bin/env sh\n# " + forgeShimTestInvoke + forgeShimTestLaunch,
			rootShim:       shim,
			canonical:      shim,
			existingStatus: bootstrap.CurrentnessInconclusive,
			stageErr:       true,
		},
		{
			name:           "heredoc mention does not claim root shim",
			runSh:          forgeShimHeredocSh,
			rootShim:       shim,
			canonical:      shim,
			existingStatus: bootstrap.CurrentnessInconclusive,
			stageErr:       true,
		},
		{
			name:           "conditional mention does not claim root shim",
			runSh:          forgeShimConditionalSh,
			rootShim:       shim,
			canonical:      shim,
			existingStatus: bootstrap.CurrentnessInconclusive,
			stageErr:       true,
		},
		{
			name:           "other directory invocation does not claim root shim",
			runSh:          "#!/usr/bin/env sh\njava -jar other/" + forgeShimTestRoot + " --onlyCheckJava || exit 1\n" + forgeShimTestLaunch,
			rootShim:       shim,
			canonical:      shim,
			existingStatus: bootstrap.CurrentnessInconclusive,
			stageErr:       true,
		},
		{
			name:           "unreferenced root file does not claim root shim",
			runSh:          "#!/usr/bin/env sh\n" + forgeShimTestLaunch,
			rootShim:       shim,
			canonical:      shim,
			existingStatus: bootstrap.CurrentnessInconclusive,
			stageErr:       true,
		},
		{
			name:           "invoked root shim must exist",
			runSh:          shimEraRunSh,
			canonical:      shim,
			existingStatus: bootstrap.CurrentnessMismatch,
			stageErr:       true,
		},
		{
			name:           "legacy launcher with extra command keeps launch command",
			runSh:          "#!/usr/bin/env sh\necho Starting\n" + forgeShimTestLaunch,
			existingStatus: bootstrap.CurrentnessInconclusive,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files := map[string][]byte{}
			maps.Copy(files, tc.extra)
			if tc.rootShim != nil {
				files[forgeShimTestRoot] = tc.rootShim
			}
			if tc.canonical != nil {
				files[forgeShimTestLib] = tc.canonical
			}
			root := writeForgeShimTree(t, tc.runSh, files)
			locked := forgeShimTestLock()

			existing, err := bootstrap.CheckExisting(root, locked)
			if err != nil {
				t.Fatalf("check existing: %v", err)
			}
			if existing.Status != tc.existingStatus {
				t.Fatalf("existing status = %s, want %s: %s", existing.Status, tc.existingStatus, existing.Reason)
			}
			if got := hasServerPath(existing.Outputs, forgeShimTestRoot); got != tc.existingClaimed {
				t.Fatalf("existing claims root shim = %t, want %t", got, tc.existingClaimed)
			}

			outputs, stageErr := bootstrap.ValidateStagedOutputs(root, locked)
			if (stageErr != nil) != tc.stageErr {
				t.Fatalf("staged verification error = %v, want error %t", stageErr, tc.stageErr)
			}
			if got := hasServerPath(outputs, forgeShimTestRoot); got != tc.stageClaimed {
				t.Fatalf("staged claims root shim = %t, want %t", got, tc.stageClaimed)
			}
		})
	}
}

// forgeShimTestLock returns a Forge lock with no declared inputs.
func forgeShimTestLock() document.LockedServer {
	return document.LockedServer{
		Directory:    ".",
		Product:      server.ProductForge,
		Minecraft:    version.Version("1.21.11"),
		CoreVersion:  version.Version("61.2.0"),
		Bootstrapper: server.BootstrapMethodForgeInstaller,
	}
}

// shimJar returns a ZIP whose manifest names the Forge shim main class, with
// the manifest layout of a real shim (CRLF endings). variant changes the bytes.
func shimJar(t *testing.T, variant string) []byte {
	t.Helper()
	return zipBytes(t, map[string][]byte{
		"META-INF/MANIFEST.MF":                         []byte("Manifest-Version: 1.0\r\nMain-Class: net.minecraftforge.bootstrap.shim.Main\r\n\r\n"),
		"net/minecraftforge/bootstrap/shim/Main.class": []byte(variant),
	})
}

// wrongMainJar returns a valid ZIP whose manifest names a different entry point.
func wrongMainJar(t *testing.T) []byte {
	t.Helper()
	return zipBytes(t, map[string][]byte{
		"META-INF/MANIFEST.MF": []byte("Manifest-Version: 1.0\r\nMain-Class: net.minecraftforge.installer.SimpleInstaller\r\n\r\n"),
	})
}

func zipBytes(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(entries[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// writeForgeShimTree writes a Forge server tree with the given run.sh and files.
func writeForgeShimTree(t *testing.T, runSh string, files map[string][]byte) string {
	t.Helper()
	root := t.TempDir()
	base := map[string][]byte{
		"run.sh":            []byte(runSh),
		"run.bat":           []byte("@echo off\r\njava @user_jvm_args.txt @" + forgeShimTestArgs + " %*\r\n"),
		"user_jvm_args.txt": []byte("-Xmx2G\n"),
		forgeShimTestArgs:   []byte("-Djava.net.preferIPv6Addresses=system\n"),
	}
	maps.Copy(base, files)
	for rel, data := range base {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func hasServerPath(outputs []bootstrap.Output, rel string) bool {
	return slices.ContainsFunc(outputs, func(out bootstrap.Output) bool {
		return out.ServerPath == rel
	})
}
