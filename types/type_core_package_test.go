package types

import "testing"

func TestCorePackageDefinitionsInvariants(t *testing.T) {
	canonicalEcos := map[CorePackage]Ecosystem{
		CoreMinecraft:        EcoMinecraft,
		CoreFabric:           EcoFabric,
		CoreForge:            EcoForge,
		CoreNeoForge:         EcoNeoforge,
		CoreMCDReforged:      EcoMcdr,
		CoreCraftBukkit:      EcoBukkit,
		CoreSpigot:           EcoBukkit,
		CorePaper:            EcoPaper,
		CoreFolia:            EcoPaper,
		CoreLeaves:           EcoPaper,
		CoreArclight:         EcoUnspecified,
		CoreArclightForge:    EcoUnspecified,
		CoreArclightNeoForge: EcoUnspecified,
		CoreArclightFabric:   EcoUnspecified,
		CoreCatServer:        EcoUnspecified,
		CoreYouer:            EcoUnspecified,
		CoreSpongeVanilla:    EcoSponge,
		CoreSpongeForge:      EcoSponge,
		CoreSpongeNeo:        EcoSponge,
		CoreBungeeCord:       EcoBungeecord,
		CoreWaterfall:        EcoBungeecord,
		CoreVelocity:         EcoVelocity,
	}

	seenCores := make(map[CorePackage]bool)
	seenAliases := make(map[PackageRef]CorePackage)
	for _, definition := range corePackageDefinitions {
		if seenCores[definition.Core] {
			t.Errorf("duplicate definition for core %q", definition.Core)
		}
		seenCores[definition.Core] = true

		wantEco, known := canonicalEcos[definition.Core]
		if !known {
			t.Errorf("core %q missing from canonical eco table", definition.Core)
			continue
		}
		if definition.Ref.Eco != wantEco {
			t.Errorf(
				"core %q canonical eco = %q, want %q",
				definition.Core, definition.Ref.Eco, wantEco,
			)
		}
		if definition.Ref.Name != BarePackageName(definition.Core) {
			t.Errorf(
				"core %q canonical name = %q, want %q",
				definition.Core, definition.Ref.Name, definition.Core,
			)
		}

		canonicalCovered := false
		for _, alias := range definition.Aliases {
			if alias.Name == "" {
				t.Errorf("core %q has alias with empty name", definition.Core)
			}
			if alias.Name != lowercasePackageName(alias.Name) {
				t.Errorf(
					"core %q alias %q is not lowercase",
					definition.Core, alias.Name,
				)
			}
			if owner, exists := seenAliases[alias]; exists {
				t.Errorf(
					"alias %q/%q claimed by both %q and %q",
					alias.Eco, alias.Name, owner, definition.Core,
				)
			}
			seenAliases[alias] = definition.Core
			if alias == definition.Ref {
				canonicalCovered = true
			}
		}
		if !canonicalCovered {
			t.Errorf(
				"core %q canonical ref %q/%q is not among its aliases",
				definition.Core, definition.Ref.Eco, definition.Ref.Name,
			)
		}
	}
	if len(seenCores) != len(canonicalEcos) {
		t.Errorf(
			"catalog defines %d cores, canonical table has %d",
			len(seenCores), len(canonicalEcos),
		)
	}
}

func TestNormalizeCorePackage(t *testing.T) {
	tests := []struct {
		name     string
		request  ScopedPackageRef
		wantCore CorePackage
		wantOk   bool
	}{
		{
			name: "bare minecraft alias",
			request: ScopedPackageRef{
				PackageRef: PackageRef{Eco: EcoUnspecified, Name: "mc"},
				Scope:      SourceAuto,
			},
			wantCore: CoreMinecraft,
			wantOk:   true,
		},
		{
			name: "case insensitive",
			request: ScopedPackageRef{
				PackageRef: PackageRef{Eco: EcoUnspecified, Name: "Paper"},
				Scope:      SourceAuto,
			},
			wantCore: CorePaper,
			wantOk:   true,
		},
		{
			name: "fabric loader alias",
			request: ScopedPackageRef{
				PackageRef: PackageRef{Eco: EcoFabric, Name: "fabric-loader"},
				Scope:      SourceAuto,
			},
			wantCore: CoreFabric,
			wantOk:   true,
		},
		{
			name: "sponge eco forge maps to spongeforge",
			request: ScopedPackageRef{
				PackageRef: PackageRef{Eco: EcoSponge, Name: "forge"},
				Scope:      SourceAuto,
			},
			wantCore: CoreSpongeForge,
			wantOk:   true,
		},
		{
			name: "regular package is not a core",
			request: ScopedPackageRef{
				PackageRef: PackageRef{Eco: EcoFabric, Name: "fabric-api"},
				Scope:      SourceAuto,
			},
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, ok := NormalizeCorePackage(tt.request)
			if ok != tt.wantOk {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOk)
			}
			if !ok {
				return
			}
			if match.Core != tt.wantCore {
				t.Errorf("core = %q, want %q", match.Core, tt.wantCore)
			}
			if match.Ref.Scope != tt.request.Scope {
				t.Errorf(
					"scope = %q, want request scope %q",
					match.Ref.Scope, tt.request.Scope,
				)
			}
		})
	}
}

func TestNormalizeCorePackagePreservesExplicitScope(t *testing.T) {
	match, ok := NormalizeCorePackage(ScopedPackageRef{
		PackageRef: PackageRef{Eco: EcoUnspecified, Name: "forge"},
		Scope:      SourceForge,
	})
	if !ok {
		t.Fatal("expected forge to normalize as a core package")
	}
	if match.Ref.Scope != SourceForge {
		t.Errorf("scope = %q, want %q", match.Ref.Scope, SourceForge)
	}
	if match.Ref.PackageRef != (PackageRef{Eco: EcoForge, Name: "forge"}) {
		t.Errorf("ref = %+v, want canonical forge ref", match.Ref.PackageRef)
	}
}
