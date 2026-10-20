package modrinth

import (
	"errors"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

var (
	ErrNoVersion = errors.New("modrinth version not found")
	ErrNoProject = errors.New("modrinth project not found")
	ErrNoMember  = errors.New("modrinth project member not found")
)

// Known crash risk: this path has caused segmentation faults.
func listVersions(slug types.PackageName) (
	versions []*versionResponse,
	err error,
) {
	var out []*versionResponse
	if err := requestJSON(
		versionsUrl(slug),
		&out,
		ErrNoProject,
	); err != nil {
		return nil, err
	}
	return out, nil
}

// getVersion is named so because a Lucy package corresponds to a Modrinth
// version; see the package terminology note.
func getVersion(
	local upstream.LocalContext,
	id types.VersionedPackageRef,
) (v *versionResponse, err error) {
	versions, err := listVersions(id.Name)
	if err != nil {
		return nil, err
	}
	if id.Version.CanInfer() {
		v, err = latestVersion(
			id.Name,
			id.Ecosystem,
			local.MinecraftVersion,
			id.Version,
		)
		if err != nil {
			return nil, err
		}
		return v, nil
	}
	if selected := selectExactVersion(versions, id, local.MinecraftVersion); selected != nil {
		return selected, nil
	}
	return nil, ErrNoVersion
}

func getVersionById(id string) (v *versionResponse, err error) {
	v = &versionResponse{}
	if err := requestJSON(versionUrl(id), v, ErrNoVersion); err != nil {
		return nil, err
	}
	return
}

func versionSupportsLoader(
	version *versionResponse,
	loader types.Ecosystem,
) bool {
	for _, l := range version.Loaders {
		if types.Ecosystem(l).Satisfy(loader) {
			return true
		}
	}
	return false
}

// latestVersion selects the newest version matching the selector. stable
// accepts releases only; beta accepts any channel; any prefers a release and
// falls back to a prerelease.
func latestVersion(
	slug types.PackageName,
	platform types.Ecosystem,
	gameVersion types.Version,
	selector types.Version,
) (v *versionResponse, err error) {
	versions, err := listVersions(slug)
	if err != nil {
		return nil, err
	}
	filterByLoader := platform != types.EcosystemUnspecified
	v = selectLatestVersionForSelector(
		versions,
		platform,
		gameVersion,
		filterByLoader,
		selector,
	)
	if v == nil {
		return nil, ErrNoVersion
	}
	log.Debug("latest version of " + slug.String() + ": " + v.VersionNumber)
	return v, nil
}
