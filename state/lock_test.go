package state

import (
	"strings"
	"testing"
)

func TestLockWithEmbeddedDependencyValidates(t *testing.T) {
	lock := Lock{
		GeneratedAt:         "2026-04-15T12:34:56Z",
		ManifestFingerprint: "sha256:manifest",
		GameVersion:         "1.21.1",
		Platform:            "neoforge",
		PlatformVersion:     "21.1.0",
		Packages: []LockedPackage{
			{
				ID: "parent-mod", Version: "1.0.0", Source: "modrinth", Platform: "neoforge",
				URL: "https://example.invalid/parent-mod.jar", Filename: "parent-mod.jar", Hash: "parenthash", HashAlgorithm: "sha512", InstallPath: "mods/parent-mod.jar", Side: "server", Provenance: []string{"root"}, Requester: "root",
			},
			{
				ID: "embedded-lib", Version: "2.0.0", Source: "direct", Platform: "neoforge",
				URL: "jar-in-jar://parent-mod.jar!/META-INF/jarjar/embedded-lib.jar", Filename: "embedded-lib.jar", Hash: "embeddedhash", HashAlgorithm: "sha512", InstallPath: "mods/parent-mod.jar!/META-INF/jarjar/embedded-lib.jar", Side: "server", Embedded: true, EmbeddedIn: "parent-mod", Provenance: []string{"root", "parent-mod@1.0.0"}, Requester: "parent-mod",
			},
		},
	}

	if err := ValidateLock(lock); err != nil {
		t.Fatalf("expected embedded package lock to validate: %v", err)
	}
}

func TestValidateLockIgnoresObservedOnlyFieldsAtStructBoundary(t *testing.T) {
	lock := Lock{
		GeneratedAt:         "2026-04-15T12:34:56Z",
		ManifestFingerprint: "sha256:manifest",
		GameVersion:         "1.21.1",
		Platform:            "fabric",
		PlatformVersion:     "0.16.10",
	}

	if err := ValidateLock(lock); err != nil {
		t.Fatalf("expected schema-level validation only, got error: %v", err)
	}

	invalidFixture := []byte("generated_at: \"2026-04-15T12:34:56Z\"\nmanifest_fingerprint: sha256:manifest\ngame_version: \"1.21.1\"\nplatform: fabric\nplatform_version: \"0.16.10\"\nplayer_count: 12\n")
	var decoded Lock
	if err := decoded.Unmarshal(invalidFixture); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}

	if err := ValidateLock(decoded); err != nil {
		t.Fatalf("expected unknown live-state field to be ignored by typed schema validation, got %v", err)
	}
	if len(decoded.Packages) != 0 {
		t.Fatalf("expected no packages in invalid fixture decode, got %d", len(decoded.Packages))
	}
}

func TestValidateLockRejectsFuzzyVersions(t *testing.T) {
	tests := []string{
		"any",
		"stable",
		"beta",
		">=0.12.0 <0.13.0",
		"^1.2.3",
		"1.2.x",
	}

	for _, version := range tests {
		t.Run(version, func(t *testing.T) {
			lock := Lock{
				GeneratedAt:         "2026-04-15T12:34:56Z",
				ManifestFingerprint: "sha256:manifest",
				GameVersion:         "1.21.1",
				Platform:            "fabric",
				PlatformVersion:     "0.16.10",
				Packages: []LockedPackage{{
					ID:            "lithium",
					Version:       version,
					Source:        "modrinth",
					Platform:      "fabric",
					URL:           "https://example.invalid/lithium.jar",
					Filename:      "lithium.jar",
					Hash:          "hash",
					HashAlgorithm: "sha512",
					InstallPath:   "mods/lithium.jar",
					Side:          "server",
					Provenance:    []string{"root"},
					Requester:     "root",
				}},
			}

			err := ValidateLock(lock)
			if err == nil {
				t.Fatalf("expected fuzzy lock version %q to be rejected", version)
			}
			if !strings.Contains(err.Error(), "version must be exact") {
				t.Fatalf("expected exact-version error, got %v", err)
			}
		})
	}
}

func TestValidateLockRejectsMissingArtifactPlatform(t *testing.T) {
	lock := Lock{
		GeneratedAt: "2026-04-15T12:34:56Z", ManifestFingerprint: "sha256:manifest",
		GameVersion: "1.21.1", Platform: "fabric", PlatformVersion: "0.16.10",
		Packages: []LockedPackage{{
			ID: "lithium", Version: "0.12.7+mc1.21.1", Source: "modrinth",
			URL: "https://example.invalid/lithium.jar", Filename: "lithium.jar", Hash: "hash", HashAlgorithm: "sha512", InstallPath: "mods/lithium.jar", Side: "server", Provenance: []string{"root"}, Requester: "root",
		}},
	}
	err := ValidateLock(lock)
	if err == nil {
		t.Fatal("expected lock validation to reject a missing artifact platform")
	}
	if !strings.Contains(err.Error(), "invalid package platform") {
		t.Fatalf("expected artifact-platform error, got %v", err)
	}
}
