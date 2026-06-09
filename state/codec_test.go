package state

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseSerializeConfigRoundTripFixture(t *testing.T) {
	fixture := mustReadStateFixture(t, "config.yaml")

	parsed, err := ParseConfig(fixture)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}

	first, err := SerializeConfig(parsed)
	if err != nil {
		t.Fatalf("SerializeConfig failed: %v", err)
	}

	parsedAgain, err := ParseConfig(first)
	if err != nil {
		t.Fatalf("ParseConfig re-read failed: %v", err)
	}

	second, err := SerializeConfig(parsedAgain)
	if err != nil {
		t.Fatalf("SerializeConfig second pass failed: %v", err)
	}

	if !reflect.DeepEqual(parsed, parsedAgain) {
		t.Fatalf("config changed after round-trip\nfirst: %#v\nsecond: %#v", parsed, parsedAgain)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("config serialization is not deterministic\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestParseSerializeManifestRoundTripFixture(t *testing.T) {
	fixture := mustReadStateFixture(t, "manifest.yaml")

	parsed, err := ParseManifest(fixture)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	first, err := SerializeManifest(parsed)
	if err != nil {
		t.Fatalf("SerializeManifest failed: %v", err)
	}

	parsedAgain, err := ParseManifest(first)
	if err != nil {
		t.Fatalf("ParseManifest re-read failed: %v", err)
	}

	second, err := SerializeManifest(parsedAgain)
	if err != nil {
		t.Fatalf("SerializeManifest second pass failed: %v", err)
	}

	if !reflect.DeepEqual(parsed, parsedAgain) {
		t.Fatalf("manifest changed after round-trip\nfirst: %#v\nsecond: %#v", parsed, parsedAgain)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("manifest serialization is not deterministic\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestParseSerializeLockRoundTripFixture(t *testing.T) {
	fixture := mustReadStateFixture(t, "lock.yaml")

	parsed, err := ParseLock(fixture)
	if err != nil {
		t.Fatalf("ParseLock failed: %v", err)
	}

	first, err := SerializeLock(parsed)
	if err != nil {
		t.Fatalf("SerializeLock failed: %v", err)
	}

	parsedAgain, err := ParseLock(first)
	if err != nil {
		t.Fatalf("ParseLock re-read failed: %v", err)
	}

	second, err := SerializeLock(parsedAgain)
	if err != nil {
		t.Fatalf("SerializeLock second pass failed: %v", err)
	}

	if !reflect.DeepEqual(parsed, parsedAgain) {
		t.Fatalf("lock changed after round-trip\nfirst: %#v\nsecond: %#v", parsed, parsedAgain)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("lock serialization is not deterministic\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestSerializeDeterministicAcrossCalls(t *testing.T) {
	configFixture := mustReadStateFixture(t, "config.yaml")
	config, err := ParseConfig(configFixture)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}

	configA, err := SerializeConfig(config)
	if err != nil {
		t.Fatalf("SerializeConfig first failed: %v", err)
	}
	configB, err := SerializeConfig(config)
	if err != nil {
		t.Fatalf("SerializeConfig second failed: %v", err)
	}
	if !bytes.Equal(configA, configB) {
		t.Fatalf("SerializeConfig output differs between runs")
	}

	manifestFixture := mustReadStateFixture(t, "manifest.yaml")
	manifest, err := ParseManifest(manifestFixture)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}

	manifestA, err := SerializeManifest(manifest)
	if err != nil {
		t.Fatalf("SerializeManifest first failed: %v", err)
	}
	manifestB, err := SerializeManifest(manifest)
	if err != nil {
		t.Fatalf("SerializeManifest second failed: %v", err)
	}
	if !bytes.Equal(manifestA, manifestB) {
		t.Fatalf("SerializeManifest output differs between runs")
	}

	lockFixture := mustReadStateFixture(t, "lock.yaml")
	lock, err := ParseLock(lockFixture)
	if err != nil {
		t.Fatalf("ParseLock failed: %v", err)
	}

	lockA, err := SerializeLock(lock)
	if err != nil {
		t.Fatalf("SerializeLock first failed: %v", err)
	}
	lockB, err := SerializeLock(lock)
	if err != nil {
		t.Fatalf("SerializeLock second failed: %v", err)
	}
	if !bytes.Equal(lockA, lockB) {
		t.Fatalf("SerializeLock output differs between runs")
	}
}

func TestParsersRejectInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		fn   func([]byte) error
	}{
		{
			name: "config",
			fn: func(data []byte) error {
				_, err := ParseConfig(data)
				return err
			},
		},
		{
			name: "manifest",
			fn: func(data []byte) error {
				_, err := ParseManifest(data)
				return err
			},
		},
		{
			name: "lock",
			fn: func(data []byte) error {
				_, err := ParseLock(data)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn([]byte("this is not valid structured state")); err == nil {
				t.Fatalf("expected parse error")
			}
		})
	}
}

func mustReadStateFixture(t *testing.T, name string) []byte {
	t.Helper()

	path := filepath.Join("testdata", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}

	return data
}
