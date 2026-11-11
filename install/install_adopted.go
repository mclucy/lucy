package install

import (
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/upstream"
)

// AdoptedRelease pairs a release the workspace already holds with the manifest
// reference that declares it.
type AdoptedRelease struct {
	Reference string
	Ecosystem catalog.Ecosystem
	Candidate upstream.Candidate
}

// adoptedCandidate answers a catalogue lookup from a release the workspace
// already holds. Adoption declares the installed version, so no catalogue
// query can select different bytes for it. The reference answers the project
// name as written; the project identity answers a demand that names it.
func adoptedCandidate(
	adopted []AdoptedRelease,
	registry catalog.Registry,
	project string,
	ecosystem catalog.Ecosystem,
) (upstream.Candidate, bool) {
	for _, release := range adopted {
		if release.Ecosystem != ecosystem {
			continue
		}
		if releaseRegistry, ok := release.Candidate.Provider.Registry(); !ok ||
			releaseRegistry != registry {
			continue
		}
		provider, name, err := document.ParseReference(release.Reference)
		if err != nil || provider != release.Candidate.Provider {
			continue
		}
		if name == project || release.Candidate.ProjectID == project {
			return release.Candidate, true
		}
	}
	return upstream.Candidate{}, false
}
