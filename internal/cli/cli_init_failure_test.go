package cli

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/operation"
	"github.com/mclucy/lucy/report"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace/discovery"
)

func cliTestRef(ecosystem catalog.Ecosystem, name, version string) catalog.VersionedRef {
	return catalog.VersionedRef{
		Ecosystem: ecosystem,
		Name:      catalog.Name(name),
		Version:   versionpkg.Version(version),
	}
}

func cliTestFabricIdentity(loader, minecraft string) discovery.Identity {
	return discovery.Identity{
		Primary: cliTestRef(catalog.EcosystemFabric, "fabric", loader),
		Components: []catalog.VersionedRef{
			cliTestRef(catalog.EcosystemFabric, "fabric-loader", loader),
			cliTestRef(catalog.EcosystemMinecraft, "minecraft", minecraft),
		},
	}
}

func cliTestVanillaIdentity(minecraft string) discovery.Identity {
	return discovery.Identity{
		Primary: cliTestRef(catalog.EcosystemMinecraft, "minecraft", minecraft),
	}
}

func cliTestInstallation(
	identity discovery.Identity,
	entryPoints ...string,
) discovery.Installation {
	installation := discovery.NewInstallation(identity, "")
	for _, path := range entryPoints {
		installation.AddFile(path, discovery.RoleEntryPoint)
	}
	return installation
}

// A refused initialization must name the exact non-interactive selection for
// every installed runtime, because the operator cannot answer a prompt.
func TestInitializationFailureNamesSelectionCommands(t *testing.T) {
	t.Parallel()

	fabricPath := "/srv/fabric-server-launch.jar"
	vanillaPath := "/srv/server.jar"
	candidates := []discovery.Installation{
		cliTestInstallation(cliTestFabricIdentity("0.19.5", "1.21.1"), fabricPath),
		cliTestInstallation(cliTestVanillaIdentity("1.21.1"), vanillaPath),
	}

	tests := []struct {
		name  string
		err   *discovery.SelectionError
		title string
	}{
		{
			name: "ambiguous runtimes",
			err: &discovery.SelectionError{
				State:         discovery.SelectionAmbiguous,
				Installations: candidates,
			},
			title: "several server runtimes share this directory",
		},
		{
			name: "missing declared installation",
			err: &discovery.SelectionError{
				State:         discovery.SelectionMissingDeclared,
				Installations: candidates,
			},
			title: "the declared server installation is missing",
		},
		{
			name: "unrecognized explicit selection",
			err: &discovery.SelectionError{
				State:         discovery.SelectionInvalid,
				Path:          "/srv/notes.txt",
				Installations: candidates,
			},
			title: "the requested entry point is not a recognized server",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			failure := initializationFailure(tt.err, nil)
			detail, ok := failure.(*report.Detail)
			if !ok {
				t.Fatalf("failure detail = %T, want *report.Detail", failure)
			}
			if detail.Title != tt.title {
				t.Fatalf("detail title = %q, want %q", detail.Title, tt.title)
			}
			want := []string{
				"lucy init --entry-point '" + fabricPath + "'",
				"lucy init --entry-point '" + vanillaPath + "'",
			}
			if !slices.Equal(detail.Lines, want) {
				t.Fatalf("detail lines = %q, want %q", detail.Lines, want)
			}
			if detail.Hint == "" {
				t.Fatalf("detail hint is empty")
			}
			if !errors.Is(detail, tt.err) {
				t.Fatalf("detail does not wrap the selection error")
			}
		})
	}
}

// Selection commands quote entry points so an operator can paste them.
func TestEntryPointCommandsQuotePaths(t *testing.T) {
	t.Parallel()

	installation := cliTestInstallation(
		cliTestFabricIdentity("0.19.5", "1.21.1"),
		"/srv/it's server.jar",
	)
	commands := entryPointCommands([]discovery.Installation{installation})
	want := []string{"lucy init --entry-point '/srv/it'\\''s server.jar'"}
	if len(commands) != 1 || commands[0] != want[0] {
		t.Fatalf("commands = %q, want %q", commands, want)
	}
}

// Failures without a typed selection still surface unreadable evidence, and
// ordinary errors pass through untouched.
func TestInitializationFailureKeepsUnreadableEvidence(t *testing.T) {
	t.Parallel()

	cause := errors.New("zip: not a valid archive")
	result := &operation.InitResult{
		Discovery: discovery.Result{
			Diagnostics: []discovery.Finding{{
				Kind:  discovery.FindingUnreadableFile,
				Path:  "/srv/server.jar",
				Cause: cause,
			}},
		},
	}
	baseErr := errors.New("no server detected; use `lucy create` to generate a new server")
	failure := initializationFailure(baseErr, result)
	detail, ok := failure.(*report.Detail)
	if !ok {
		t.Fatalf("failure detail = %T, want *report.Detail", failure)
	}
	if detail.Title != baseErr.Error() {
		t.Fatalf("detail title = %q, want %q", detail.Title, baseErr)
	}
	if len(detail.Lines) != 1 || !strings.Contains(detail.Lines[0], cause.Error()) {
		t.Fatalf("detail lines = %q, want the unreadable cause", detail.Lines)
	}
	if !errors.Is(detail, baseErr) {
		t.Fatalf("detail does not wrap the command error")
	}

	if got := initializationFailure(baseErr, nil); got != baseErr {
		t.Fatalf("plain failure = %v, want the original error", got)
	}
}

// Discovery outcomes reach the operator with a severity that matches whether
// a runtime was selected.
func TestReportDiscoveryFindingsPresentsSelectionOutcomes(t *testing.T) {
	t.Parallel()

	fabricPath := "/srv/fabric-server-launch.jar"
	shimPath := "/srv/fabric-server-shim.jar"
	vanillaPath := "/srv/server.jar"

	t.Run("automatic selection warns with competitors", func(t *testing.T) {
		t.Parallel()

		result := discovery.Result{Installations: []discovery.Installation{
			cliTestInstallation(cliTestFabricIdentity("0.19.5", "1.21.1"), fabricPath),
			cliTestInstallation(cliTestVanillaIdentity("1.21.1"), vanillaPath),
		}}
		if err := result.Select(discovery.Request{Policy: discovery.PolicyPreferModified}); err != nil {
			t.Fatalf("select preferred runtime: %v", err)
		}
		cmd, _, stderr := newStreamCommand()
		output := NewOperationOutput(cmd, newTestInvocation())
		reportDiscoveryFindings(&result, output)

		warnings := output.Warnings()
		if len(warnings) != 1 {
			t.Fatalf("warnings = %q, want the automatic selection", warnings)
		}
		for _, fragment := range []string{
			fabricPath,
			vanillaPath,
			"automatically",
			"lucy init --entry-point",
		} {
			if !strings.Contains(warnings[0], fragment) {
				t.Fatalf("warning %q does not name %q", warnings[0], fragment)
			}
		}
		if !strings.Contains(stderr.String(), "warning:") {
			t.Fatalf("human warning missing from stderr:\n%s", stderr.String())
		}
	})

	t.Run("preserved entry point reports at info", func(t *testing.T) {
		t.Parallel()

		fabric := cliTestInstallation(
			cliTestFabricIdentity("0.19.5", "1.21.1"),
			shimPath,
			fabricPath,
		)
		result := discovery.Result{Installations: []discovery.Installation{fabric}}
		if err := result.Select(discovery.Request{}); err != nil {
			t.Fatalf("select inferred runtime: %v", err)
		}
		cmd, _, stderr := newStreamCommand()
		output := NewOperationOutput(cmd, newTestInvocation())
		reportDiscoveryFindings(&result, output)

		if len(output.Warnings()) != 0 {
			t.Fatalf("resolved layout warned: %q", output.Warnings())
		}
		if !strings.Contains(stderr.String(), "INFO") ||
			!strings.Contains(stderr.String(), shimPath) {
			t.Fatalf("info finding missing from stderr:\n%s", stderr.String())
		}
	})

	t.Run("missing declaration warns", func(t *testing.T) {
		t.Parallel()

		result := discovery.Result{Installations: []discovery.Installation{
			cliTestInstallation(cliTestVanillaIdentity("1.21.1"), vanillaPath),
		}}
		err := result.Select(discovery.Request{Declared: &discovery.Declaration{
			Product:     "fabric",
			Minecraft:   "1.21.1",
			CoreVersion: "0.19.5",
		}})
		if err == nil {
			t.Fatalf("selection adopted a substitute runtime")
		}
		cmd, _, stderr := newStreamCommand()
		output := NewOperationOutput(cmd, newTestInvocation())
		reportDiscoveryFindings(&result, output)

		warnings := output.Warnings()
		if len(warnings) != 1 ||
			!strings.Contains(warnings[0], "declared server installation is missing") {
			t.Fatalf("warnings = %q, want the missing-declaration warning", warnings)
		}
		if !strings.Contains(stderr.String(), "warning:") {
			t.Fatalf("human warning missing from stderr:\n%s", stderr.String())
		}
	})

	t.Run("ambiguous layout names selection commands", func(t *testing.T) {
		t.Parallel()

		result := discovery.Result{Installations: []discovery.Installation{
			cliTestInstallation(cliTestFabricIdentity("0.19.5", "1.21.1"), fabricPath),
			cliTestInstallation(cliTestVanillaIdentity("1.21.1"), vanillaPath),
		}}
		if err := result.Select(discovery.Request{Policy: discovery.PolicyStrict}); err == nil {
			t.Fatalf("strict selection accepted competing runtimes")
		}
		cmd, _, stderr := newStreamCommand()
		output := NewOperationOutput(cmd, newTestInvocation())
		reportDiscoveryFindings(&result, output)

		for _, fragment := range []string{
			"cannot choose between server runtimes",
			"lucy init --entry-point '" + fabricPath + "'",
			"lucy init --entry-point '" + vanillaPath + "'",
		} {
			if !strings.Contains(stderr.String(), fragment) {
				t.Fatalf("stderr does not name %q:\n%s", fragment, stderr.String())
			}
		}
	})
}
