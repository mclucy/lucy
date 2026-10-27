package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mclucy/lucy/catalog"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/terminal"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/spf13/cobra"
)

// Captured with `lucy status --json` from generated sandboxes. The
// machine-specific root path is normalized to ".".
const (
	statusPackagesFixture = "status-packages.json" // create-plus-1211, 106 packages
	statusWarningsFixture = "status-warnings.json" // fabric-executable-1214
)

func loadStatusFixture(t *testing.T, name string) statusDocument {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var document statusDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	if document.Root != "." {
		t.Fatalf("%s root = %q, want normalized \".\"", name, document.Root)
	}
	return document
}

func TestStatusRenderLineWidths(t *testing.T) {
	document := loadStatusFixture(t, statusPackagesFixture)
	modes := []terminal.StatusLogoMode{
		terminal.StatusLogoNone,
		terminal.StatusLogoSmall,
		terminal.StatusLogoLarge,
	}

	for _, long := range []bool{false, true} {
		for _, width := range []int{40, 80, 120} {
			for _, mode := range modes {
				rendered := renderStatus(document, long, mode, width)
				for _, line := range strings.Split(rendered, "\n") {
					if columns := ansi.StringWidth(line); columns > width {
						t.Fatalf(
							"long=%t mode=%v width=%d: line %q is %d columns wide",
							long, mode, width, line, columns,
						)
					}
				}
			}
		}
	}
}

func TestStatusRenderZeroWidth(t *testing.T) {
	document := loadStatusFixture(t, statusPackagesFixture)

	for _, long := range []bool{false, true} {
		want := renderStatus(document, long, terminal.StatusLogoNone, 80)
		for _, width := range []int{0, -12} {
			got := renderStatus(document, long, terminal.StatusLogoNone, width)
			if got != want {
				t.Fatalf(
					"long=%t width=%d differs from width 80:\n%q\n%q",
					long, width, got, want,
				)
			}
		}
	}
}

func TestStatusRenderLongPackages(t *testing.T) {
	document := loadStatusFixture(t, statusPackagesFixture)
	short := renderStatus(document, false, terminal.StatusLogoNone, 120)
	long := renderStatus(document, true, terminal.StatusLogoNone, 120)

	const packagePath = "mods/create-1.21.1-6.0.10.jar"
	if !strings.Contains(long, packagePath) {
		t.Fatalf("long output omits package path %q:\n%s", packagePath, long)
	}
	if strings.Contains(short, "mods/") {
		t.Fatalf("folded output should omit package paths:\n%s", short)
	}
	if !strings.Contains(short, "use --long") {
		t.Fatalf("folded output should advertise --long:\n%s", short)
	}
}

func TestStatusRenderLogoNone(t *testing.T) {
	document := loadStatusFixture(t, statusPackagesFixture)
	rendered := renderStatus(document, false, terminal.StatusLogoNone, 80)

	for _, want := range []string{
		"Minecraft",
		"1.21.1",
		"NeoForge",
		"aeroencasedpipe",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered status omits %q:\n%s", want, rendered)
		}
	}

	for _, variant := range []terminal.LogoVariant{
		terminal.LogoSmallPlain,
		terminal.LogoLargePlain,
	} {
		signature := statusRenderLogoSignature(variant)
		if signature != "" && strings.Contains(rendered, signature) {
			t.Fatalf("logo mode none rendered %q from variant %v", signature, variant)
		}
	}
}

func TestStatusWarningSurfaced(t *testing.T) {
	document := loadStatusFixture(t, statusWarningsFixture)
	if len(document.Warnings) == 0 {
		t.Fatal("warning fixture has no warnings")
	}
	warning := document.Warnings[0]

	var stream bytes.Buffer
	command := &cobra.Command{Use: "status"}
	command.SetOut(&stream)
	command.SetErr(&stream)

	output := NewQueryOutput(command)
	output.Warn(errors.New(warning))

	if err := writeStatusDocument(
		output,
		document,
		false,
		terminal.StatusLogoNone,
	); err != nil {
		t.Fatalf("write status: %v", err)
	}

	rendered := stream.String()
	if !strings.Contains(rendered, warning) {
		t.Fatalf("rendered status omits warning %q:\n%s", warning, rendered)
	}
	if !strings.Contains(rendered, "Fabric") {
		t.Fatalf("rendered status frame missing after warning:\n%s", rendered)
	}
}

func statusRenderLogoSignature(variant terminal.LogoVariant) string {
	raw := terminal.GetLogo(
		serverpkg.ProductNeoForge,
		catalog.EcosystemNeoForge,
		versionpkg.Version("1.21.1"),
		variant,
	)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(ansi.Strip(line))
		if len(line) >= 12 && strings.ContainsAny(line, "{}[]%@#") {
			return line
		}
	}
	return ""
}
