package bootstrap

import (
	"testing"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
)

func loaderTestServer(
	primary types.VersionedPackageRef,
	components ...types.VersionedPackageRef,
) *workspace.ServerInstance {
	return &workspace.ServerInstance{
		PrimaryRuntime:    &primary,
		PrimaryPath:       "server.jar",
		RuntimeComponents: components,
	}
}

func loaderTestRef(
	eco types.Ecosystem,
	name string,
) types.VersionedPackageRef {
	return types.VersionedPackageRef{
		PackageRef: types.PackageRef{
			Eco:  eco,
			Name: types.BarePackageName(name),
		},
		Version: "1.0.0",
	}
}

func TestSelectedLoader(t *testing.T) {
	tests := []struct {
		name   string
		server *workspace.ServerInstance
		want   types.Ecosystem
	}{
		{
			name:   "nil server",
			server: nil,
			want:   types.EcoUnspecified,
		},
		{
			name: "vanilla",
			server: loaderTestServer(
				loaderTestRef(types.EcoMinecraft, "minecraft"),
			),
			want: types.EcoUnspecified,
		},
		{
			name: "fabric via primary runtime",
			server: loaderTestServer(
				loaderTestRef(types.EcoFabric, "fabric"),
			),
			want: types.EcoFabric,
		},
		{
			name: "arclight claims loader via component",
			server: loaderTestServer(
				loaderTestRef(types.EcoUnspecified, "arclight"),
				loaderTestRef(types.EcoFabric, "fabric-loader"),
			),
			want: types.EcoFabric,
		},
		{
			name: "catserver hybrid claims forge without loader component",
			server: loaderTestServer(
				loaderTestRef(types.EcoUnspecified, "catserver"),
				loaderTestRef(types.EcoMinecraft, "minecraft"),
			),
			want: types.EcoForge,
		},
		{
			name: "youer hybrid claims neoforge",
			server: loaderTestServer(
				loaderTestRef(types.EcoUnspecified, "youer"),
				loaderTestRef(types.EcoMinecraft, "minecraft"),
			),
			want: types.EcoNeoforge,
		},
		{
			name: "paper server claims no loader",
			server: loaderTestServer(
				loaderTestRef(types.EcoUnspecified, "paper"),
			),
			want: types.EcoUnspecified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := selectedLoader(tt.server); got != tt.want {
				t.Errorf("selectedLoader() = %v, want %v", got, tt.want)
			}
		})
	}
}
