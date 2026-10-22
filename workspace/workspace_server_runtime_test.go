package workspace

import (
	"testing"

	"github.com/mclucy/lucy/catalog"
	versionpkg "github.com/mclucy/lucy/version"
)

func componentRef(
	eco catalog.Ecosystem,
	name string,
	version versionpkg.Version,
) catalog.VersionedRef {
	return catalog.VersionedRef{
		Ecosystem: eco,
		Name:      catalog.Name(name),
		Version:   version,
	}
}

func TestNormalizeRuntimeComponents(t *testing.T) {
	tests := []struct {
		name  string
		input []catalog.VersionedRef
		want  []catalog.VersionedRef
	}{
		{
			name: "unknown version loader survives",
			input: []catalog.VersionedRef{
				componentRef(catalog.EcosystemForge, "forge", versionpkg.Unknown),
			},
			want: []catalog.VersionedRef{
				componentRef(catalog.EcosystemForge, "forge", versionpkg.Unknown),
			},
		},
		{
			name: "concrete version wins over unknown duplicate",
			input: []catalog.VersionedRef{
				componentRef(catalog.EcosystemForge, "forge", versionpkg.Unknown),
				componentRef(catalog.EcosystemForge, "forge", "47.2.0"),
			},
			want: []catalog.VersionedRef{
				componentRef(catalog.EcosystemForge, "forge", "47.2.0"),
			},
		},
		{
			name: "unknown duplicate does not disturb concrete",
			input: []catalog.VersionedRef{
				componentRef(catalog.EcosystemForge, "forge", "47.2.0"),
				componentRef(catalog.EcosystemForge, "forge", versionpkg.Unknown),
			},
			want: []catalog.VersionedRef{
				componentRef(catalog.EcosystemForge, "forge", "47.2.0"),
			},
		},
		{
			name: "conflicting concrete versions are dropped",
			input: []catalog.VersionedRef{
				componentRef(catalog.EcosystemForge, "forge", "47.2.0"),
				componentRef(catalog.EcosystemForge, "forge", "48.0.1"),
			},
			want: []catalog.VersionedRef{},
		},
		{
			name: "inferable versions are rejected",
			input: []catalog.VersionedRef{
				componentRef(catalog.EcosystemForge, "forge", versionpkg.Any),
				componentRef(catalog.EcosystemMinecraft, "minecraft", ""),
			},
			want: []catalog.VersionedRef{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeRuntimeComponents(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("components = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf(
						"component[%d] = %v, want %v",
						i, got[i], tt.want[i],
					)
				}
			}
		})
	}
}
