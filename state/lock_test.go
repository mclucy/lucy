package state

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLockRoundTrip(t *testing.T) {
	original := Lock{
		Version:             "v1",
		GeneratedAt:         "2026-04-15T12:34:56Z",
		ManifestFingerprint: "sha256:manifest",
		GameVersion:         "1.21.1",
		Platform:            "fabric",
		PlatformVersion:     "0.16.10",
		Packages: []LockedPackage{
			{
				ID:            "fabric/fabric-api",
				Version:       "0.110.5+1.21.1",
				Source:        "modrinth",
				URL:           "https://cdn.modrinth.com/data/P7dR8mSH/versions/abc/fabric-api.jar",
				Filename:      "fabric-api-0.110.5+1.21.1.jar",
				Hash:          "deadbeef",
				HashAlgorithm: "sha512",
				InstallPath:   "mods/fabric-api-0.110.5+1.21.1.jar",
				Side:          "both",
				Optional:      false,
				Embedded:      false,
				EmbeddedIn:    "",
				Provenance:    []string{"root"},
				Requester:     "root",
			},
		},
		Bundles: []LockedBundle{
			{
				Name:        "default-config",
				Type:        "config-overlay",
				Hash:        "cafebabe",
				InstallPath: "config/defaults.zip",
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded Lock
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Version != original.Version ||
		decoded.GeneratedAt != original.GeneratedAt ||
		decoded.ManifestFingerprint != original.ManifestFingerprint ||
		decoded.GameVersion != original.GameVersion ||
		decoded.Platform != original.Platform ||
		decoded.PlatformVersion != original.PlatformVersion {
		t.Fatalf("top-level fields changed after round-trip: %#v", decoded)
	}

	if len(decoded.Packages) != 1 {
		t.Fatalf("expected 1 package after round-trip, got %d", len(decoded.Packages))
	}

	pkg := decoded.Packages[0]
	if pkg.ID != original.Packages[0].ID ||
		pkg.Version != original.Packages[0].Version ||
		pkg.Source != original.Packages[0].Source ||
		pkg.URL != original.Packages[0].URL ||
		pkg.Filename != original.Packages[0].Filename ||
		pkg.Hash != original.Packages[0].Hash ||
		pkg.HashAlgorithm != original.Packages[0].HashAlgorithm ||
		pkg.InstallPath != original.Packages[0].InstallPath ||
		pkg.Side != original.Packages[0].Side ||
		pkg.Requester != original.Packages[0].Requester {
		t.Fatalf("package fields changed after round-trip: %#v", pkg)
	}

	if len(pkg.Provenance) != 1 || pkg.Provenance[0] != "root" {
		t.Fatalf("provenance changed after round-trip: %#v", pkg.Provenance)
	}

	if len(decoded.Bundles) != 1 {
		t.Fatalf("expected 1 bundle after round-trip, got %d", len(decoded.Bundles))
	}

	bundle := decoded.Bundles[0]
	if bundle != original.Bundles[0] {
		t.Fatalf("bundle changed after round-trip: %#v", bundle)
	}
}

func TestLockEmbeddedDependency(t *testing.T) {
	lock := Lock{
		Version:             "v1",
		GeneratedAt:         "2026-04-15T12:34:56Z",
		ManifestFingerprint: "sha256:manifest",
		GameVersion:         "1.21.1",
		Platform:            "neoforge",
		PlatformVersion:     "21.1.0",
		Packages: []LockedPackage{
			{
				ID:            "neoforge/parent-mod",
				Version:       "1.0.0",
				Source:        "modrinth",
				URL:           "https://example.invalid/parent-mod.jar",
				Filename:      "parent-mod.jar",
				Hash:          "parenthash",
				HashAlgorithm: "sha512",
				InstallPath:   "mods/parent-mod.jar",
				Side:          "server",
				Provenance:    []string{"root"},
				Requester:     "root",
			},
			{
				ID:            "neoforge/embedded-lib",
				Version:       "2.0.0",
				Source:        "direct",
				URL:           "jar-in-jar://parent-mod.jar!/META-INF/jarjar/embedded-lib.jar",
				Filename:      "embedded-lib.jar",
				Hash:          "embeddedhash",
				HashAlgorithm: "sha512",
				InstallPath:   "mods/parent-mod.jar!/META-INF/jarjar/embedded-lib.jar",
				Side:          "server",
				Embedded:      true,
				EmbeddedIn:    "neoforge/parent-mod",
				Provenance:    []string{"root", "neoforge/parent-mod@1.0.0"},
				Requester:     "neoforge/parent-mod",
			},
		},
	}

	if err := ValidateLock(lock); err != nil {
		t.Fatalf("expected embedded package lock to validate: %v", err)
	}

	if len(lock.Packages) != 2 {
		t.Fatalf("expected embedded dependency to remain in packages slice, got %d entries", len(lock.Packages))
	}

	if lock.Packages[1].Embedded != true {
		t.Fatalf("expected embedded dependency to stay marked embedded")
	}

	if lock.Packages[1].EmbeddedIn != lock.Packages[0].ID {
		t.Fatalf("expected embedded dependency parent linkage %q, got %q", lock.Packages[0].ID, lock.Packages[1].EmbeddedIn)
	}
}

func TestLockProvenanceRoundTrip(t *testing.T) {
	original := Lock{
		Version:             "v1",
		GeneratedAt:         "2026-04-15T12:34:56Z",
		ManifestFingerprint: "sha256:manifest",
		GameVersion:         "1.21.1",
		Platform:            "fabric",
		PlatformVersion:     "0.16.10",
		Packages: []LockedPackage{
			{
				ID:            "fabric/sodium",
				Version:       "0.6.0",
				Source:        "modrinth",
				URL:           "https://example.invalid/sodium.jar",
				Filename:      "sodium.jar",
				Hash:          "hash",
				HashAlgorithm: "sha512",
				InstallPath:   "mods/sodium.jar",
				Side:          "both",
				Provenance: []string{
					"root",
					"fabric/fabric-api@0.110.5+1.21.1",
					"fabric/indium@1.0.35+mc1.21",
				},
				Requester: "fabric/indium",
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded Lock
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	got := decoded.Packages[0].Provenance
	if len(got) != 3 {
		t.Fatalf("expected 3 provenance nodes, got %d", len(got))
	}

	for i, want := range original.Packages[0].Provenance {
		if got[i] != want {
			t.Fatalf("expected provenance[%d]=%q, got %q", i, want, got[i])
		}
	}
}

func TestValidateLockIgnoresObservedOnlyFieldsAtStructBoundary(t *testing.T) {
	lock := Lock{
		Version:             "v1",
		GeneratedAt:         "2026-04-15T12:34:56Z",
		ManifestFingerprint: "sha256:manifest",
		GameVersion:         "1.21.1",
		Platform:            "fabric",
		PlatformVersion:     "0.16.10",
	}

	if err := ValidateLock(lock); err != nil {
		t.Fatalf("expected schema-level validation only, got error: %v", err)
	}

	invalidFixture := []byte(`{"version":"v1","generated_at":"2026-04-15T12:34:56Z","manifest_fingerprint":"sha256:manifest","game_version":"1.21.1","platform":"fabric","platform_version":"0.16.10","player_count":12}`)
	var decoded Lock
	if err := json.Unmarshal(invalidFixture, &decoded); err != nil {
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
		"latest",
		"compatible",
		">=0.12.0 <0.13.0",
		"^1.2.3",
		"1.2.x",
	}

	for _, version := range tests {
		t.Run(version, func(t *testing.T) {
			lock := Lock{
				Version:             "v1",
				GeneratedAt:         "2026-04-15T12:34:56Z",
				ManifestFingerprint: "sha256:manifest",
				GameVersion:         "1.21.1",
				Platform:            "fabric",
				PlatformVersion:     "0.16.10",
				Packages: []LockedPackage{{
					ID:            "fabric/lithium",
					Version:       version,
					Source:        "modrinth",
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

func TestValidateLockRejectsNonExactPackageIdentityFacts(t *testing.T) {
	lock := Lock{
		Version:             "v1",
		GeneratedAt:         "2026-04-15T12:34:56Z",
		ManifestFingerprint: "sha256:manifest",
		GameVersion:         "1.21.1",
		Platform:            "fabric",
		PlatformVersion:     "0.16.10",
		Packages: []LockedPackage{{
			ID:            "lithium",
			Version:       "0.12.7+mc1.21.1",
			Source:        "modrinth",
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
		t.Fatal("expected lock validation to reject package ids without exact platform facts")
	}
	if !strings.Contains(err.Error(), "platform/name format") {
		t.Fatalf("expected exact package identity error, got %v", err)
	}
}
