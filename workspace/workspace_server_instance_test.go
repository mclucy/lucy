package workspace

import (
	"testing"

	"github.com/mclucy/lucy/types"
)

func componentRef(
	eco types.Ecosystem,
	name string,
	version types.BareVersion,
) types.VersionedPackageRef {
	return types.VersionedPackageRef{
		Eco:     eco,
		Name:    types.BarePackageName(name),
		Version: version,
	}
}

func TestNormalizeRuntimeComponents(t *testing.T) {
	tests := []struct {
		name  string
		input []types.VersionedPackageRef
		want  []types.VersionedPackageRef
	}{
		{
			name: "unknown version loader survives",
			input: []types.VersionedPackageRef{
				componentRef(types.EcoForge, "forge", types.VersionUnknown),
			},
			want: []types.VersionedPackageRef{
				componentRef(types.EcoForge, "forge", types.VersionUnknown),
			},
		},
		{
			name: "concrete version wins over unknown duplicate",
			input: []types.VersionedPackageRef{
				componentRef(types.EcoForge, "forge", types.VersionUnknown),
				componentRef(types.EcoForge, "forge", "47.2.0"),
			},
			want: []types.VersionedPackageRef{
				componentRef(types.EcoForge, "forge", "47.2.0"),
			},
		},
		{
			name: "unknown duplicate does not disturb concrete",
			input: []types.VersionedPackageRef{
				componentRef(types.EcoForge, "forge", "47.2.0"),
				componentRef(types.EcoForge, "forge", types.VersionUnknown),
			},
			want: []types.VersionedPackageRef{
				componentRef(types.EcoForge, "forge", "47.2.0"),
			},
		},
		{
			name: "conflicting concrete versions are dropped",
			input: []types.VersionedPackageRef{
				componentRef(types.EcoForge, "forge", "47.2.0"),
				componentRef(types.EcoForge, "forge", "48.0.1"),
			},
			want: []types.VersionedPackageRef{},
		},
		{
			name: "inferable versions are rejected",
			input: []types.VersionedPackageRef{
				componentRef(types.EcoForge, "forge", types.VersionAny),
				componentRef(types.EcoMinecraft, "minecraft", ""),
			},
			want: []types.VersionedPackageRef{},
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
