package install

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/artifactstore"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/receipt"
	"github.com/mclucy/lucy/server"
)

// A receipt that does not own a required managed bootstrap output must not
// claim currency, even when the receipt's fingerprint and the file itself are
// intact. Otherwise the output is never restaged and never reported unowned.
func TestBootstrapCurrentRequiresReceiptOwnershipOfManagedOutput(t *testing.T) {
	root := t.TempDir()
	core := []byte("plain server core\n")
	transactionTestWrite(t, root, "server/server.jar", core)
	lock := &document.Lock{
		SchemaVersion: document.LockSchemaVersion,
		Server: document.LockedServer{
			Product:   server.ProductMinecraft,
			Minecraft: "1.21.4",
			Directory: "server",
			Bootstrap: server.BootstrapMethodVanilla,
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderMojang,
				ProjectID: "server",
				Version:   "1.21.4",
				Filename:  "server.jar",
				Hashes:    map[string]string{"sha256": transactionTestDigest(core)},
			},
		},
	}
	fingerprint, err := receipt.BootstrapFingerprint(lock)
	if err != nil {
		t.Fatalf("bootstrap fingerprint: %v", err)
	}

	partial := receipt.New()
	partial.BootstrapFingerprint = fingerprint
	current, err := bootstrapCurrent(root, lock, &partial, nil)
	if err != nil {
		t.Fatalf("partial receipt check: %v", err)
	}
	if current {
		t.Fatal("receipt without an entry for server/server.jar reported current")
	}

	owned := receipt.New()
	owned.BootstrapFingerprint = fingerprint
	owned.Entries = []receipt.Entry{{
		Path:   "server/server.jar",
		SHA256: transactionTestDigest(core),
		Origin: receipt.OriginBootstrap,
	}}
	current, err = bootstrapCurrent(root, lock, &owned, nil)
	if err != nil {
		t.Fatalf("owned receipt check: %v", err)
	}
	if !current {
		t.Fatal("receipt owning server/server.jar with its baseline reported not current")
	}
}

func TestBootstrapCurrentRequiresForgeInstallerEvidence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "appdata"))

	const (
		args = "libraries/net/minecraftforge/forge/1.21.11-61.2.0/unix_args.txt"
		lib  = "libraries/required.jar"
	)
	library := []byte("processor output")
	var installer bytes.Buffer
	zw := zip.NewWriter(&installer)
	profile, err := zw.Create("install_profile.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(profile, `{"processors":[{"outputs":{"{ROOT}/%s":"%x"}}]}`, lib, sha1.Sum(library)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	lock := &document.Lock{
		Server: document.LockedServer{
			Product:     server.ProductForge,
			Minecraft:   "1.21.11",
			CoreVersion: "61.2.0",
			Bootstrap:   server.BootstrapMethodForge,
			Directory:   "server",
			Artifact: catalog.Artifact{
				Hashes: map[string]string{"sha256": transactionTestDigest(installer.Bytes())},
			},
		},
	}
	root := t.TempDir()
	installed := receipt.New()
	installed.BootstrapFingerprint, err = receipt.BootstrapFingerprint(lock)
	if err != nil {
		t.Fatal(err)
	}
	own := func(rel string, data []byte) {
		t.Helper()
		path := "server/" + rel
		transactionTestWrite(t, root, path, data)
		installed.Entries = append(installed.Entries, receipt.Entry{
			Path:   path,
			Origin: receipt.OriginBootstrap,
			SHA256: transactionTestDigest(data),
		})
	}
	own("run.sh", []byte("#!/bin/sh\njava @user_jvm_args.txt @"+args+" \"$@\"\n"))
	own("run.bat", []byte("@echo off\r\njava @user_jvm_args.txt @"+args+" %*\r\n"))
	own(args, []byte("-Djava.net.preferIPv6Addresses=system\n"))
	transactionTestWrite(t, root, "server/user_jvm_args.txt", []byte("-Xmx2G\n"))

	check := func(want bool, scenario string) {
		t.Helper()
		got, err := bootstrapCurrent(root, lock, &installed, nil)
		if err != nil || got != want {
			t.Errorf("%s: current = %v, error = %v, want %v", scenario, got, err, want)
		}
	}
	check(false, "missing installer and no receipt entry for its required library")
	store, err := artifactstore.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.Put(bytes.NewReader(installer.Bytes()), nil); err != nil {
		t.Fatal(err)
	}
	check(false, "pinned installer declares a missing library")
	own(lib, library)
	check(true, "declared library and unhashed launch arguments match the receipt")
}
