package types

import "testing"

func TestCorePackageDefinitionsInvariants(t *testing.T) {
	wantEco := map[CorePackage]Ecosystem{
		CoreMinecraft: EcoMinecraft, CoreFabric: EcoFabric, CoreForge: EcoForge,
		CoreNeoForge: EcoNeoforge, CoreMCDReforged: EcoMcdr,
		CoreCraftBukkit: EcoBukkit, CoreSpigot: EcoBukkit, CorePaper: EcoPaper,
		CoreFolia: EcoPaper, CoreLeaves: EcoPaper,
		CoreArclight: EcoUnspecified, CoreArclightForge: EcoUnspecified,
		CoreArclightNeoForge: EcoUnspecified, CoreArclightFabric: EcoUnspecified,
		CoreCatServer: EcoUnspecified, CoreYouer: EcoUnspecified,
		CoreSpongeVanilla: EcoSponge, CoreSpongeForge: EcoSponge, CoreSpongeNeo: EcoSponge,
		CoreBungeeCord: EcoBungeecord, CoreVelocity: EcoVelocity, CoreWaterfall: EcoBungeecord,
	}
	seenCores := make(map[CorePackage]bool)
	seenAliases := make(map[corePackageAlias]CorePackage)
	for _, definition := range corePackageDefinitions {
		if seenCores[definition.Core] {
			t.Errorf("duplicate definition for core %q", definition.Core)
		}
		seenCores[definition.Core] = true
		if definition.Eco != wantEco[definition.Core] {
			t.Errorf("core %q eco = %q, want %q", definition.Core, definition.Eco, wantEco[definition.Core])
		}
		for _, alias := range definition.Aliases {
			if alias.Name == "" {
				t.Errorf("core %q has empty alias", definition.Core)
			}
			if alias.Name != lowercasePackageName(alias.Name) {
				t.Errorf("core %q alias %q is not lowercase", definition.Core, alias.Name)
			}
			if owner, exists := seenAliases[alias]; exists {
				t.Errorf("alias %q/%q claimed by %q and %q", alias.Eco, alias.Name, owner, definition.Core)
			}
			seenAliases[alias] = definition.Core
		}
	}
	if len(seenCores) != len(wantEco) {
		t.Errorf("catalog defines %d cores, expected %d", len(seenCores), len(wantEco))
	}
}

func TestNormalizeCorePackage(t *testing.T) {
	tests := []struct {
		name    string
		request PackageRequest
		want    CorePackage
		ok      bool
	}{
		{"bare minecraft alias", PackageRequest{PackageRef: PackageRef{Name: "mc", Source: SourceAuto}}, CoreMinecraft, true},
		{"case insensitive", PackageRequest{PackageRef: PackageRef{Name: "Paper", Source: SourceAuto}}, CorePaper, true},
		{"fabric loader alias", PackageRequest{PackageRef: PackageRef{Name: "fabric-loader", Source: SourceAuto}, Eco: EcoFabric}, CoreFabric, true},
		{"sponge forge alias", PackageRequest{PackageRef: PackageRef{Name: "forge", Source: SourceAuto}, Eco: EcoSponge}, CoreSpongeForge, true},
		{"regular package", PackageRequest{PackageRef: PackageRef{Name: "fabric-api", Source: SourceAuto}, Eco: EcoFabric}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeCorePackage(tt.request)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && got.Core != tt.want {
				t.Errorf("core = %q, want %q", got.Core, tt.want)
			}
		})
	}
}
