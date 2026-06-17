package forge

import (
	"reflect"
	"testing"
)

func TestParseSupportedMinecraftVersionsPreservesPromotionOrder(t *testing.T) {
	versions, err := parseSupportedMinecraftVersions([]byte(`{
		"promos": {
			"1.20.1-latest": "47.3.22",
			"26.1.2-latest": "64.0.0",
			"1.18.2-latest": "40.2.17",
			"1.20.1-recommended": "47.3.21"
		}
	}`))
	if err != nil {
		t.Fatalf("parseSupportedMinecraftVersions returned error: %v", err)
	}

	want := []string{"1.20.1", "1.18.2"}
	if !reflect.DeepEqual(versions, want) {
		t.Fatalf("unexpected versions: got %v want %v", versions, want)
	}
}
