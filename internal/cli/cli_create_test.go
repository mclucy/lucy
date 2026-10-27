package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// runCreateCommand executes create under the minimal root the command needs,
// with prompts disabled, so it fails on its own validation.
func runCreateCommand(t *testing.T, args ...string) error {
	t.Helper()

	root := &cobra.Command{Use: "lucy"}
	root.SilenceErrors = true
	root.SilenceUsage = true
	root.PersistentFlags().Bool(FlagNoInteractive, false, "Disable prompts")
	root.AddCommand(NewCreateCommand(NewJSONContracts()))
	root.SetIn(nonTerminalInput(t))
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	root.SetArgs(args)
	return root.Execute()
}

// assertCreateTargetUntouched proves the failed command wrote nothing.
func assertCreateTargetUntouched(t *testing.T, directory string) {
	t.Helper()

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read target directory: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("create touched %s: %q", directory, names)
	}
}

func TestCreateRequiresMinecraft(t *testing.T) {
	t.Run("missing minecraft version", func(t *testing.T) {
		directory := t.TempDir()
		err := runCreateCommand(
			t,
			"create",
			"--directory", directory,
			"--no-interactive",
		)
		if err == nil {
			t.Fatal("create succeeded without a Minecraft version")
		}
		if !strings.Contains(err.Error(), "--minecraft") {
			t.Fatalf("error %q does not name --minecraft", err)
		}
		assertCreateTargetUntouched(t, directory)
	})

	t.Run("install contradiction", func(t *testing.T) {
		directory := t.TempDir()
		err := runCreateCommand(
			t,
			"create",
			"--directory", directory,
			"--no-interactive",
			"--install",
			"--no-install",
		)
		if err == nil {
			t.Fatal("create accepted --install together with --no-install")
		}
		for _, flag := range []string{"--install", "--no-install"} {
			if !strings.Contains(err.Error(), flag) {
				t.Fatalf("error %q does not name %s", err, flag)
			}
		}
		assertCreateTargetUntouched(t, directory)
	})
}
