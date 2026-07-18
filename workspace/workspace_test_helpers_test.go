package workspace

import (
	"testing"

	"github.com/mclucy/lucy/types"
)

func makeDiscoveredPackage(
	t *testing.T,
	platform types.Ecosystem,
	name, version, path string,
) types.DiscoveredPackage {
	t.Helper()
	return types.DiscoveredPackage{
		Id: types.VersionedPackageRef{
			PackageRef: types.PackageRef{
				Eco:  platform,
				Name: types.BarePackageName(name),
			},
			Version: types.BareVersion(version),
		},
		Path: path,
	}
}
