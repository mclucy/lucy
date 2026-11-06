package bootstrap_test

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/server"
)

func TestBootstrapArtifactDigests(t *testing.T) {
	t.Parallel()
	data := []byte("bootstrap artifact")
	md5sum := md5.Sum(data)
	sha1sum := sha1.Sum(data)
	sha256sum := sha256.Sum256(data)
	sha512sum := sha512.Sum512(data)
	md5hex := hex.EncodeToString(md5sum[:])
	sha1hex := hex.EncodeToString(sha1sum[:])
	sha256hex := hex.EncodeToString(sha256sum[:])
	sha512hex := hex.EncodeToString(sha512sum[:])

	cases := []struct {
		name    string
		hashes  map[string]string
		matches bool
	}{
		{name: "md5 only", hashes: map[string]string{"md5": md5hex}, matches: true},
		{name: "sha512 only", hashes: map[string]string{"sha512": sha512hex}, matches: true},
		{name: "sha1 only", hashes: map[string]string{"sha1": sha1hex}, matches: true},
		{name: "sha256 only", hashes: map[string]string{"sha256": sha256hex}, matches: true},
		{name: "all pins", hashes: map[string]string{
			"md5": md5hex, "sha1": sha1hex, "sha256": strings.ToUpper(sha256hex), "sha512": sha512hex,
		}, matches: true},
		{name: "conflicting md5", hashes: map[string]string{
			"sha256": sha256hex, "md5": strings.Repeat("0", 32),
		}},
		{name: "conflicting sha512", hashes: map[string]string{
			"sha256": sha256hex, "sha512": strings.Repeat("0", 128),
		}},
		{name: "unsupported digest", hashes: map[string]string{"sha256": sha256hex, "unknown": "abc"}},
		{name: "empty pins", hashes: map[string]string{"md5": ""}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "server.jar"), data, 0o644); err != nil {
				t.Fatal(err)
			}
			locked := document.LockedServer{
				Directory:    ".",
				Product:      server.ProductMinecraft,
				Artifact:     catalog.Artifact{Hashes: tc.hashes},
				Bootstrapper: server.BootstrapMethodPlain,
			}
			existing, err := bootstrap.CheckExisting(root, locked)
			if err != nil {
				t.Fatalf("check existing: %v", err)
			}
			if existing.Current != tc.matches {
				t.Fatalf("existing runtime current = %t, want %t: %s", existing.Current, tc.matches, existing.Reason)
			}
			outputs, stageErr := bootstrap.ValidateStagedOutputs(root, locked)
			if (stageErr == nil) != tc.matches {
				t.Fatalf("stage verification error = %v, want match %t", stageErr, tc.matches)
			}
			if tc.matches && (len(existing.Outputs) != 1 || existing.Outputs[0].SHA256 != sha256hex ||
				len(outputs) != 1 || outputs[0].SHA256 != sha256hex) {
				t.Fatalf("verified outputs have wrong SHA-256: existing=%v staged=%v", existing.Outputs, outputs)
			}
		})
	}
}
