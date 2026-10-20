package types

import "testing"

func TestServerProductDefinitionsInvariants(t *testing.T) {
	seenProducts := make(map[ServerProduct]bool)
	seenAliases := make(map[serverProductAlias]ServerProduct)
	for _, definition := range serverProductDefinitions {
		if seenProducts[definition.Product] {
			t.Errorf("duplicate definition for product %q", definition.Product)
		}
		seenProducts[definition.Product] = true
		for _, alias := range definition.Aliases {
			if alias.Name == "" {
				t.Errorf("product %q has empty alias", definition.Product)
			}
			if alias.Name != PackageName(lowercasePackageName(alias.Name)) {
				t.Errorf("product %q alias %q is not lowercase", definition.Product, alias.Name)
			}
			if owner, exists := seenAliases[alias]; exists {
				t.Errorf("alias %q/%q claimed by %q and %q", alias.Ecosystem, alias.Name, owner, definition.Product)
			}
			seenAliases[alias] = definition.Product
		}
	}
}

func TestNormalizeServerProduct(t *testing.T) {
	tests := []struct {
		name    string
		request VersionedPackageRef
		want    ServerProduct
		ok      bool
	}{
		{
			"bare minecraft alias",
			VersionedPackageRef{Name: "mc", Provider: ProviderUnspecified},
			ServerProductMinecraft,
			true,
		},
		{
			"case insensitive",
			VersionedPackageRef{Name: "Paper", Provider: ProviderUnspecified},
			ServerProductPaper,
			true,
		},
		{
			"fabric loader alias",
			VersionedPackageRef{
				Name:      "fabric-loader",
				Provider:  ProviderUnspecified,
				Ecosystem: EcosystemFabric,
			},
			ServerProductFabric,
			true,
		},
		{
			"sponge forge alias",
			VersionedPackageRef{
				Name:      "forge",
				Provider:  ProviderUnspecified,
				Ecosystem: EcosystemSponge,
			},
			ServerProductSpongeForge,
			true,
		},
		{
			"regular package",
			VersionedPackageRef{
				Name:      "fabric-api",
				Provider:  ProviderUnspecified,
				Ecosystem: EcosystemFabric,
			},
			"",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeServerProduct(tt.request)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && got.Product != tt.want {
				t.Errorf("product = %q, want %q", got.Product, tt.want)
			}
		})
	}
}

func lowercasePackageName(name PackageName) string {
	lowered := ""
	for _, r := range string(name) {
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		lowered += string(r)
	}
	return lowered
}
