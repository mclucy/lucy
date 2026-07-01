package types

import "testing"

func TestEcosystem_Satisfy_PaperImpliesBukkit(t *testing.T) {
	if !EcoPaper.Satisfy(EcoBukkit) {
		t.Fatal("paper offering should satisfy bukkit package requirement")
	}
	if EcoBukkit.Satisfy(EcoPaper) {
		t.Fatal("bukkit offering must not satisfy paper package requirement")
	}
}

func TestEcosystem_Satisfy_SameModLoader(t *testing.T) {
	if !EcoFabric.Satisfy(EcoFabric) {
		t.Fatal("fabric should satisfy fabric")
	}
	if EcoFabric.Satisfy(EcoForge) {
		t.Fatal("fabric must not satisfy forge")
	}
}

func TestEcosystem_Satisfy_SpecialSelectors(t *testing.T) {
	if !EcoFabric.Satisfy(EcoBare) {
		t.Fatal("bare requirement satisfied by any offering")
	}
	if EcoUnknown.Satisfy(EcoFabric) {
		t.Fatal("unknown satisfies nothing")
	}
	if !EcoFabric.Satisfy(EcoAny) {
		t.Fatal("any requirement satisfied by concrete ecosystem")
	}
	if EcoAny.Satisfy(EcoFabric) {
		t.Fatal("any offering satisfies no concrete requirement")
	}
}
