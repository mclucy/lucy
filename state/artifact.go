package state

type ArtifactRelationKind string

const RelationEmbedded ArtifactRelationKind = "embedded"

type ArtifactRelation struct {
	ParentID string
	ChildID  string
	Kind     ArtifactRelationKind
}

type ManagedArtifact struct {
	ID            string
	Version       string
	InstallPath   string
	Class         ArtifactClass
	Side          string
	Optional      bool
	Embedded      bool
	EmbeddedIn    string
	Provenance    []string
	Source        string
	Hash          string
	HashAlgorithm string
}

type BundleArtifact struct {
	Name        string
	Type        string
	InstallPath string
	Hash        string
}

type ArtifactSet struct {
	Packages  []ManagedArtifact
	Bundles   []BundleArtifact
	Relations []ArtifactRelation
}

func LockToArtifactSet(lock *Lock) ArtifactSet {
	if lock == nil {
		return ArtifactSet{}
	}

	scope := NewManagedScope(nil, nil)
	artifacts := ArtifactSet{
		Packages:  make([]ManagedArtifact, 0, len(lock.Packages)),
		Bundles:   make([]BundleArtifact, 0, len(lock.Bundles)),
		Relations: make([]ArtifactRelation, 0),
	}

	for _, pkg := range lock.Packages {
		class := ClassifyPath(scope, pkg.InstallPath)
		if pkg.Embedded {
			class = ClassEmbedded
		}

		artifacts.Packages = append(artifacts.Packages, ManagedArtifact{
			ID:            pkg.ID,
			Version:       pkg.Version,
			InstallPath:   pkg.InstallPath,
			Class:         class,
			Side:          pkg.Side,
			Optional:      pkg.Optional,
			Embedded:      pkg.Embedded,
			EmbeddedIn:    pkg.EmbeddedIn,
			Provenance:    append([]string(nil), pkg.Provenance...),
			Source:        pkg.Source,
			Hash:          pkg.Hash,
			HashAlgorithm: pkg.HashAlgorithm,
		})

		if pkg.Embedded && pkg.EmbeddedIn != "" {
			artifacts.Relations = append(artifacts.Relations, ArtifactRelation{
				ParentID: pkg.EmbeddedIn,
				ChildID:  pkg.ID,
				Kind:     RelationEmbedded,
			})
		}
	}

	for _, bundle := range lock.Bundles {
		artifacts.Bundles = append(artifacts.Bundles, BundleArtifact{
			Name:        bundle.Name,
			Type:        bundle.Type,
			InstallPath: bundle.InstallPath,
			Hash:        bundle.Hash,
		})
	}

	return artifacts
}

func ManagedPackages(as ArtifactSet) []ManagedArtifact {
	packages := make([]ManagedArtifact, 0, len(as.Packages))
	for _, pkg := range as.Packages {
		if pkg.Embedded || pkg.Class == ClassUnmanaged {
			continue
		}
		packages = append(packages, pkg)
	}
	return packages
}

func EmbeddedPackages(as ArtifactSet) []ManagedArtifact {
	packages := make([]ManagedArtifact, 0, len(as.Packages))
	for _, pkg := range as.Packages {
		if !pkg.Embedded {
			continue
		}
		packages = append(packages, pkg)
	}
	return packages
}
