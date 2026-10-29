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
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/terminal"
	terminalstyle "github.com/mclucy/lucy/terminal/style"
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

// renderStatusForTest exercises the renderer with explicit terminal
// capabilities instead of the process streams.
func renderStatusForTest(
	document statusDocument,
	long bool,
	mode terminal.StatusLogoMode,
	width int,
	isTerminal bool,
) string {
	return renderStatus(
		document,
		long,
		mode,
		terminalstyle.Terminal{Width: width, Height: 24, IsTTY: isTerminal},
		terminalstyle.New(false),
	)
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
				rendered := renderStatusForTest(document, long, mode, width, true)
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
		want := renderStatusForTest(document, long, terminal.StatusLogoNone, 80, true)
		for _, width := range []int{0, -12} {
			got := renderStatusForTest(document, long, terminal.StatusLogoNone, width, true)
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
	short := renderStatusForTest(document, false, terminal.StatusLogoNone, 120, true)
	long := renderStatusForTest(document, true, terminal.StatusLogoNone, 120, true)

	if len(document.Server.PackageDirectories) == 0 {
		t.Fatal("fixture declares no package directory")
	}
	directory := document.Server.PackageDirectories[0] + "/"
	packagePath := ""
	for _, pkg := range document.Packages {
		if strings.HasPrefix(pkg.Path, directory) {
			packagePath = pkg.Path
			break
		}
	}
	if packagePath == "" {
		t.Fatalf("fixture holds no package under %s", directory)
	}
	if !strings.Contains(long, packagePath) {
		t.Fatalf("long output omits package path %q:\n%s", packagePath, long)
	}
	if strings.Contains(short, directory) {
		t.Fatalf("folded output should omit package paths:\n%s", short)
	}
	if !strings.Contains(short, "use --long") {
		t.Fatalf("folded output should advertise --long:\n%s", short)
	}
}

func TestStatusRenderLogoNone(t *testing.T) {
	document := loadStatusFixture(t, statusPackagesFixture)
	rendered := renderStatusForTest(document, false, terminal.StatusLogoNone, 80, true)

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

	plain := ansi.Strip(rendered)
	for _, variant := range []terminal.LogoVariant{
		terminal.LogoSmallPlain,
		terminal.LogoLargePlain,
	} {
		lines := statusRenderLogoLines(document, variant)
		if len(lines) == 0 {
			t.Fatalf("variant %v has no logo art to check", variant)
		}
		for _, line := range lines {
			line = strings.TrimSpace(ansi.Strip(line))
			if line == "" {
				continue
			}
			if strings.Contains(plain, line) {
				t.Fatalf("logo mode none rendered %q from variant %v", line, variant)
			}
		}
	}
}

func TestStatusRenderLogoModes(t *testing.T) {
	document := loadStatusFixture(t, statusPackagesFixture)

	none := renderStatusForTest(document, false, terminal.StatusLogoNone, 120, true)
	small := renderStatusForTest(document, false, terminal.StatusLogoSmall, 120, true)
	large := renderStatusForTest(document, false, terminal.StatusLogoLarge, 120, true)

	if small == none {
		t.Fatal("logo mode small did not compose a logo")
	}
	if large == none {
		t.Fatal("logo mode large did not compose a logo")
	}
	if large == small {
		t.Fatal("logo mode large rendered the small logo")
	}
}

func TestStatusWarningPrecedesFrame(t *testing.T) {
	document := loadStatusFixture(t, statusWarningsFixture)
	const warning = "unmanaged entry point ./fabric-server-mc.1.21.4-loader.0.16.9-launcher.1.0.1.jar; left unchanged"

	var stream bytes.Buffer
	command := &cobra.Command{Use: "status"}
	command.SetOut(&stream)
	command.SetErr(&stream)

	inv := newTestInvocation()
	output := NewQueryOutput(command, inv)
	output.Warn(errors.New(warning))

	if err := writeStatusDocument(
		t.Context(),
		output,
		inv,
		document,
		false,
		terminal.StatusLogoNone,
	); err != nil {
		t.Fatalf("write status: %v", err)
	}

	rendered := stream.String()
	warningAt := strings.Index(rendered, warning)
	if warningAt < 0 {
		t.Fatalf("rendered status omits warning %q:\n%s", warning, rendered)
	}
	frameAt := strings.Index(rendered, "Fabric")
	if frameAt < 0 {
		t.Fatalf("rendered status frame missing after warning:\n%s", rendered)
	}
	if frameAt < warningAt {
		t.Fatalf("status frame precedes its warning:\n%s", rendered)
	}
}

// statusRenderLogoLines reports the logo art the document selects.
func statusRenderLogoLines(document statusDocument, variant terminal.LogoVariant) []string {
	if document.Server == nil {
		return nil
	}
	product, ok := serverpkg.ParseProduct(document.Server.Product.Name)
	if !ok {
		return nil
	}
	raw := terminal.GetLogo(
		product,
		statusViewLogoEcosystem(document.Server),
		versionpkg.Version(document.Server.Minecraft),
		variant,
	)
	return statusViewLogoLines(raw)
}
