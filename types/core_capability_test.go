package types

import "testing"

func TestCore_SupportedEcosystems_PaperIncludesBukkit(t *testing.T) {
	caps := CorePaper.SupportedEcosystems()
	if len(caps) < 2 {
		t.Fatalf("paper capability: %v", caps)
	}
	if caps[0] != EcoPaper || caps[1] != EcoBukkit {
		t.Fatalf("got %v", caps)
	}
}
