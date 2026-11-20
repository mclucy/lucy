//go:build ignore

// Command ci-bin-size-report builds the pull request and base binaries for
// every published target and writes a Markdown size comparison. CI mounts the
// checkouts as siblings: pr/ for the pull request and main/ for the base.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type target struct {
	name   string
	goos   string
	goarch string
	ext    string
}

var targets = []target{
	{name: "linux-amd64", goos: "linux", goarch: "amd64"},
	{name: "linux-arm64", goos: "linux", goarch: "arm64"},
	{name: "darwin-arm64", goos: "darwin", goarch: "arm64"},
	{name: "darwin-amd64", goos: "darwin", goarch: "amd64"},
	{name: "windows-amd64", goos: "windows", goarch: "amd64", ext: ".exe"},
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "ci-bin-size-report: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	prRoot, err := os.Getwd()
	if err != nil {
		return err
	}
	mainRoot := filepath.Join(filepath.Dir(prRoot), "main")
	prBins := "/tmp/pr-bins"
	mainBins := "/tmp/main-bins"
	report := "/tmp/report.md"

	if err := buildCheckout(prRoot, prBins); err != nil {
		return err
	}
	if err := buildCheckout(mainRoot, mainBins); err != nil {
		return err
	}

	var body strings.Builder
	body.WriteString("### 📦 Binary Size Report (compared against `main`)\n\n")
	body.WriteString("| Target | `main` | PR | Change |\n| :--- | :--- | :--- | :--- |\n")
	for _, item := range targets {
		base, err := fileSize(filepath.Join(mainBins, "lucy-"+item.name+item.ext))
		if err != nil {
			return err
		}
		head, err := fileSize(filepath.Join(prBins, "lucy-"+item.name+item.ext))
		if err != nil {
			return err
		}
		diff := head - base

		sign := ""
		if diff > 0 {
			sign = "+"
		}
		absolute := diff
		if absolute < 0 {
			absolute = -absolute
		}
		var hundredths int64
		if base > 0 {
			hundredths = (absolute*20000 + base) / (2 * base)
		}
		pctSign := ""
		if diff < 0 {
			pctSign = "-"
		}
		percent := fmt.Sprintf("%d.%02d", hundredths/100, hundredths%100)

		indicator := "⚪"
		if diff < 0 {
			indicator = "🟢"
		} else if diff > 0 {
			indicator = "🔴"
		}

		fmt.Fprintf(&body, "| `%s` | %s (%s B) | %s (%s B) | **%s%s (%s%s%%)** %s |\n",
			item.name,
			formatSize(base), groupDigits(base),
			formatSize(head), groupDigits(head),
			sign, formatSize(diff), pctSign, percent, indicator)
	}
	body.WriteString("\n*Automated check triggered by changes to `go.mod` / `go.sum`.*\n")
	body.WriteString("<!-- lucy-binary-size-report -->\n")

	if err := os.WriteFile(report, []byte(body.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", report, err)
	}
	if summary := os.Getenv("GITHUB_STEP_SUMMARY"); summary != "" {
		file, err := os.OpenFile(summary, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("open GITHUB_STEP_SUMMARY: %w", err)
		}
		defer file.Close()
		if _, err := file.WriteString(body.String()); err != nil {
			return fmt.Errorf("write GITHUB_STEP_SUMMARY: %w", err)
		}
	}
	return nil
}

func buildCheckout(root, out string) error {
	docs := exec.Command("go", "run", "./tools/gendocs")
	docs.Dir = root
	docs.Stdout = os.Stdout
	docs.Stderr = os.Stderr
	if err := docs.Run(); err != nil {
		return fmt.Errorf("generate docs in %s: %w", root, err)
	}

	if err := os.MkdirAll(out, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", out, err)
	}
	for _, item := range targets {
		build := exec.Command("go", "build", "-trimpath",
			"-ldflags", "-w -s",
			"-tags", "release,docsarchive,nethttpomithttp2",
			"-o", filepath.Join(out, "lucy-"+item.name+item.ext))
		build.Dir = root
		build.Env = append(os.Environ(), "GOOS="+item.goos, "GOARCH="+item.goarch)
		build.Stdout = os.Stdout
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			return fmt.Errorf("build %s in %s: %w", item.name, root, err)
		}
	}
	return nil
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("stat %s: %w", path, err)
	}
	return info.Size(), nil
}

func formatSize(size int64) string {
	sign := ""
	if size < 0 {
		sign = "-"
		size = -size
	}
	switch {
	case size >= 1048576:
		hundredths := (size*100 + 524288) / 1048576
		return fmt.Sprintf("%s%d.%02d MB", sign, hundredths/100, hundredths%100)
	case size >= 1024:
		tenths := (size*10 + 512) / 1024
		return fmt.Sprintf("%s%d.%d KB", sign, tenths/10, tenths%10)
	default:
		return fmt.Sprintf("%s%d B", sign, size)
	}
}

func groupDigits(size int64) string {
	digits := strconv.FormatInt(size, 10)
	var grouped strings.Builder
	for index := range len(digits) {
		if index > 0 && (len(digits)-index)%3 == 0 {
			grouped.WriteByte(',')
		}
		grouped.WriteByte(digits[index])
	}
	return grouped.String()
}
