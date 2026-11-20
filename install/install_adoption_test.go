package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mclucy/lucy/receipt"
)

func TestAdoptionRefusesVanishedAcceptedPackage(t *testing.T) {
	root, _, lock := testWorkspace(t)
	holdWorkspaceLock(t, root)
	const relative = "mods/accepted.jar"
	err := AdoptExistingWorkspace(root, AdoptOptions{
		Lock: lock,
		Packages: []AdoptedPackage{{
			Path:           relative,
			ExpectedSHA256: strings.Repeat("a", 64),
			ExpectedMode:   0o644,
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "package file "+relative+" disappeared") {
		t.Fatalf("AdoptExistingWorkspace() error = %v, want a missing package conflict", err)
	}
	if _, err := os.Stat(filepath.Join(root, receipt.Filename)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("receipt after failed adoption: %v, want absent", err)
	}
}
