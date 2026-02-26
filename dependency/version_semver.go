package dependency

import (
	"fmt"

	semverlib "github.com/Masterminds/semver/v3"
	"github.com/mclucy/lucy/types"
)

// SemverVersion implements types.ComparableVersion using Masterminds semver.
type SemverVersion semverlib.Version

// NewSemver creates a SemverVersion from explicit major, minor, patch values.
func NewSemver(major, minor, patch uint64) types.ComparableVersion {
	v, err := semverlib.StrictNewVersion(
		fmt.Sprintf("%d.%d.%d", major, minor, patch),
	)
	if err != nil {
		return nil
	}
	return (*SemverVersion)(v)
}

// parseSemver parses a semver string.
func parseSemver(s types.RawVersion) types.ComparableVersion {
	v, err := semverlib.NewVersion(string(s))
	if err != nil {
		return nil
	}
	return (*SemverVersion)(v)
}

func (s *SemverVersion) Major() uint64 {
	if s == nil {
		return 0
	}
	return (*semverlib.Version)(s).Major()
}

func (s *SemverVersion) Minor() uint64 {
	if s == nil {
		return 0
	}
	return (*semverlib.Version)(s).Minor()
}

func (s *SemverVersion) Patch() uint64 {
	if s == nil {
		return 0
	}
	return (*semverlib.Version)(s).Patch()
}

func (s *SemverVersion) Prerelease() string {
	if s == nil {
		return ""
	}
	return (*semverlib.Version)(s).Prerelease()
}

func (s *SemverVersion) Scheme() types.VersionScheme {
	return types.Semver
}

func (s *SemverVersion) Compare(other types.ComparableVersion) (int, bool) {
	if s == nil || other == nil {
		return 0, false
	}
	o, ok := other.(*SemverVersion)
	if !ok || o == nil {
		return 0, false
	}
	return (*semverlib.Version)(s).Compare((*semverlib.Version)(o)), true
}

func (s *SemverVersion) Validate() bool {
	if s == nil {
		return false
	}
	return s.Major() != 0 || s.Minor() != 0 || s.Patch() != 0
}

func (s *SemverVersion) String() string {
	if s == nil {
		return ""
	}
	return (*semverlib.Version)(s).Original()
}
