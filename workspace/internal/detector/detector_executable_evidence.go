package detector

import "github.com/mclucy/lucy/catalog"

// RuntimeEvidence is the detector output consumed by workspace assembly.
// PrimaryRuntime identifies the selected bootable artifact. A nil primary does
// not establish an executable runtime.

type RuntimeEvidence struct {
	PrimaryRuntime    *catalog.VersionedRef
	PrimaryPath       string
	RuntimeComponents []catalog.VersionedRef

	// ConsumedFiles are artifacts absorbed by this runtime, such as the
	// vanilla jar a Fabric launch shim boots.
	ConsumedFiles []*DetectionFile

	// EntryPoints are absolute paths to every launchable artifact of an
	// installation layout. Per-file detectors leave this nil; the directory
	// scan falls back to PrimaryPath.
	EntryPoints []string

	// InstallationFiles are absolute paths to every file an installation
	// detector positively proved belongs to the layout: entry points,
	// sidecars, payload jars and referenced classpath libraries. A directory
	// scan claims these files and does not run per-file detection on them.
	InstallationFiles []string

	DetectorName string
}

// IsVanilla reports evidence claiming a plain Minecraft runtime.
func (e *RuntimeEvidence) IsVanilla() bool {
	return e != nil && e.PrimaryRuntime != nil &&
		e.PrimaryRuntime.Ecosystem == catalog.EcosystemMinecraft &&
		e.PrimaryRuntime.Name == "minecraft"
}

// RuntimeClaims groups all detector candidates for one executable so the
// aggregator can resolve ambiguity before building the ServerRuntime.
type RuntimeClaims struct {
	Candidates []*RuntimeEvidence
}

func (c *RuntimeClaims) IsEmpty() bool {
	return c == nil || len(c.Candidates) == 0
}

func (c *RuntimeClaims) IsAmbiguous() bool {
	return c != nil && len(c.disambiguated()) > 1
}

func (c *RuntimeClaims) Single() *RuntimeEvidence {
	filtered := c.disambiguated()
	if len(filtered) != 1 {
		return nil
	}
	return filtered[0]
}

// disambiguated drops generic vanilla fallback evidence when a more specific
// executable detector also matched the same artifact.
func (c *RuntimeClaims) disambiguated() []*RuntimeEvidence {
	if c == nil || len(c.Candidates) <= 1 {
		if c == nil {
			return nil
		}
		return c.Candidates
	}

	hasSpecific := false
	for _, cand := range c.Candidates {
		if !cand.IsVanilla() {
			hasSpecific = true
			break
		}
	}
	if !hasSpecific {
		return c.Candidates
	}

	disambiguated := make([]*RuntimeEvidence, 0, len(c.Candidates))
	for _, cand := range c.Candidates {
		if !cand.IsVanilla() {
			disambiguated = append(disambiguated, cand)
		}
	}
	return disambiguated
}
