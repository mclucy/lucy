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

// setStdoutTerminal pins the process-global terminal probe so the renderer
// takes the same layout branch wherever the test runs.
func setStdoutTerminal(t *testing.T, isTerminal bool) {
	t.Helper()
	previous := terminalstyle.StdoutIsTerminal
	terminalstyle.StdoutIsTerminal = isTerminal
	t.Cleanup(func() { terminalstyle.StdoutIsTerminal = previous })
}

func TestStatusRenderLineWidths(t *testing.T) {
	setStdoutTerminal(t, true)
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
	setStdoutTerminal(t, true)
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
	setStdoutTerminal(t, true)
	document := loadStatusFixture(t, statusPackagesFixture)
	short := renderStatus(document, false, terminal.StatusLogoNone, 120)
	long := renderStatus(document, true, terminal.StatusLogoNone, 120)

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
	setStdoutTerminal(t, true)
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
	setStdoutTerminal(t, true)
	document := loadStatusFixture(t, statusPackagesFixture)

	none := renderStatus(document, false, terminal.StatusLogoNone, 120)
	small := renderStatus(document, false, terminal.StatusLogoSmall, 120)
	large := renderStatus(document, false, terminal.StatusLogoLarge, 120)

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
	setStdoutTerminal(t, true)
	document := loadStatusFixture(t, statusWarningsFixture)
	const warning = "unmanaged entry point ./fabric-server-mc.1.21.4-loader.0.16.9-launcher.1.0.1.jar; left unchanged"

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
