//go:build integration

package file_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

var lucyBinary string

func TestMain(m *testing.M) {
	if os.Getenv(helperModeEnv) != "" {
		os.Exit(m.Run())
	}
	code := func() int {
		directory, err := os.MkdirTemp("", "lucy-file-integration-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer os.RemoveAll(directory)
		lucyBinary = filepath.Join(directory, "lucy")
		if runtime.GOOS == "windows" {
			lucyBinary += ".exe"
		}
		// The test process runs in the package directory, so the checkout
		// root is two levels up regardless of how the source tree is
		// recorded in the binary.
		cliRoot, err := filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if _, err := os.Stat(filepath.Join(cliRoot, "go.mod")); err != nil {
			fmt.Fprintln(os.Stderr, "cannot locate CLI integration workspace")
			return 1
		}
		arguments := []string{"build"}
		if lucyRaceInstrumented {
			arguments = append(arguments, "-race")
		}
		arguments = append(arguments, "-tags", "debug", "-o", lucyBinary, ".")
		command := exec.Command("go", arguments...)
		command.Dir = cliRoot
		if output, err := command.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "build integration CLI: %v\n%s", err, output)
			return 1
		}
		return m.Run()
	}()
	os.Exit(code)
}
