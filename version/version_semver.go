package version

import (
	"encoding/json/jsontext"
	"fmt"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/types"
)

// SemverVersion implements the superset of SemVer that Fabric Loader parses:
// one or more numeric components, an optional prerelease, and arbitrary build
// metadata. Build metadata never participates in comparison.
type SemverVersion struct {
	numbers       []uint64
	prerelease    string
	hasPrerelease bool
	original      string
}

// StringVersion is the fallback Fabric Loader uses for versions outside its
// semantic grammar. Such versions compare by their full string.
type StringVersion struct {
	original string
}

func NewSemver(major, minor, patch uint64) types.ResolvableVersion {
	return &SemverVersion{
		numbers:  []uint64{major, minor, patch},
		original: fmt.Sprintf("%d.%d.%d", major, minor, patch),
	}
}

// parseSemver parses a version under the semantic grammar without a plain-string
// fallback. Schemes shared by several ecosystems, such as MCDR, require it.
func parseSemver(s types.Version) types.ResolvableVersion {
	raw := string(s)
	if raw == "" {
		return nil
	}
	if parsed := parseFabricSemver(raw); parsed != nil {
		return parsed
	}
	return nil
}

// parseFabricVersion parses a Fabric Loader version. Text outside the semantic
// grammar stays a valid version that compares as a plain string.
func parseFabricVersion(s types.Version) types.ResolvableVersion {
	raw := string(s)
	if raw == "" {
		return nil
	}
	if parsed := parseFabricSemver(raw); parsed != nil {
		return parsed
	}
	return &StringVersion{original: raw}
}

// parseFabricSemver mirrors Fabric Loader's semantic parser. It splits build
// metadata at the first '+', splits the prerelease at the first '-', and then
// requires every remaining dot-separated component to be an integer.
func parseFabricSemver(raw string) *SemverVersion {
	core := raw
	if index := strings.IndexByte(core, '+'); index >= 0 {
		core = core[:index]
	}
	prerelease := ""
	hasPrerelease := false
	if index := strings.IndexByte(core, '-'); index >= 0 {
		prerelease = core[index+1:]
		hasPrerelease = true
		core = core[:index]
		if !validPrerelease(prerelease) {
			return nil
		}
	}
	if core == "" || strings.HasPrefix(core, ".") || strings.HasSuffix(core, ".") {
		return nil
	}
	parts := strings.Split(core, ".")
	numbers := make([]uint64, 0, len(parts))
	for _, part := range parts {
		number, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return nil
		}
		numbers = append(numbers, number)
	}
	return &SemverVersion{
		numbers:       numbers,
		prerelease:    prerelease,
		hasPrerelease: hasPrerelease,
		original:      raw,
	}
}

// validPrerelease accepts the identifiers Fabric permits. An empty prerelease
// is valid and orders below every non-empty prerelease of the same core.
func validPrerelease(prerelease string) bool {
	if prerelease == "" {
		return true
	}
	for identifier := range strings.SplitSeq(prerelease, ".") {
		if identifier == "" {
			return false
		}
		for _, ch := range identifier {
			if ch != '-' &&
				(ch < '0' || ch > '9') &&
				(ch < 'A' || ch > 'Z') &&
				(ch < 'a' || ch > 'z') {
				return false
			}
		}
	}
	return true
}

func (s *SemverVersion) component(index int) uint64 {
	if s == nil || index >= len(s.numbers) {
		return 0
	}
	return s.numbers[index]
}

func (s *SemverVersion) Major() uint64 {
	return s.component(0)
}

func (s *SemverVersion) Minor() uint64 {
	return s.component(1)
}

func (s *SemverVersion) Patch() uint64 {
	return s.component(2)
}

func (s *SemverVersion) Prerelease() string {
	if s == nil {
		return ""
	}
	return s.prerelease
}

func (s *SemverVersion) Scheme() types.VersionScheme {
	return types.VersionSchemeSemver
}

func (s *SemverVersion) Compare(other types.ResolvableVersion) (int, bool) {
	if s == nil || other == nil {
		return 0, false
	}
	var o *SemverVersion
	switch other := other.(type) {
	case *SemverVersion:
		o = other
	case *StringVersion:
		return strings.Compare(s.String(), other.String()), true
	default:
		return 0, false
	}
	if o == nil {
		return 0, false
	}
	count := max(len(s.numbers), len(o.numbers))
	for index := range count {
		if comparison := compareUint(s.component(index), o.component(index)); comparison != 0 {
			return comparison, true
		}
	}
	return compareFabricPrerelease(s, o), true
}

// compareFabricPrerelease follows Fabric Loader: a release outranks its
// prereleases, numeric identifiers rank below alphanumeric ones, numeric
// identifiers compare by length, and a longer identifier list outranks a
// shorter list sharing its prefix.
func compareFabricPrerelease(left, right *SemverVersion) int {
	if !left.hasPrerelease && !right.hasPrerelease {
		return 0
	}
	if !left.hasPrerelease {
		return 1
	}
	if !right.hasPrerelease {
		return -1
	}
	leftParts := prereleaseIdentifiers(left.prerelease)
	rightParts := prereleaseIdentifiers(right.prerelease)
	for index := range min(len(leftParts), len(rightParts)) {
		if comparison := comparePrereleaseIdentifier(leftParts[index], rightParts[index]); comparison != 0 {
			return comparison
		}
	}
	return compareInt(len(leftParts), len(rightParts))
}

func prereleaseIdentifiers(prerelease string) []string {
	if prerelease == "" {
		return nil
	}
	return strings.Split(prerelease, ".")
}

func comparePrereleaseIdentifier(left, right string) int {
	leftNumeric := isUnsignedInteger(left)
	rightNumeric := isUnsignedInteger(right)
	switch {
	case leftNumeric && rightNumeric:
		if comparison := compareInt(len(left), len(right)); comparison != 0 {
			return comparison
		}
		return strings.Compare(left, right)
	case leftNumeric:
		return -1
	case rightNumeric:
		return 1
	default:
		return strings.Compare(left, right)
	}
}

func isUnsignedInteger(value string) bool {
	if value == "0" {
		return true
	}
	if value == "" || value[0] == '0' {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func compareUint(left, right uint64) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func (s *SemverVersion) Validate() bool {
	if s == nil {
		return false
	}
	for _, number := range s.numbers {
		if number != 0 {
			return true
		}
	}
	return false
}

func (s *SemverVersion) String() string {
	if s == nil {
		return ""
	}
	return s.original
}

// MarshalJSONTo renders the version in its original string form. The parsed
// components are unexported, which encoding/json/v2 refuses to marshal.
func (s *SemverVersion) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.String(s.String()))
}

func (v *StringVersion) Scheme() types.VersionScheme {
	return types.VersionSchemeSemver
}

func (v *StringVersion) Compare(other types.ResolvableVersion) (int, bool) {
	if v == nil || other == nil {
		return 0, false
	}
	switch other := other.(type) {
	case *StringVersion:
		return strings.Compare(v.original, other.original), true
	case *SemverVersion:
		return strings.Compare(v.original, other.original), true
	default:
		return 0, false
	}
}

func (v *StringVersion) Validate() bool {
	return v != nil && v.original != ""
}

func (v *StringVersion) String() string {
	if v == nil {
		return ""
	}
	return v.original
}

// MarshalJSONTo renders the version verbatim for the same reason as
// SemverVersion.
func (v *StringVersion) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.String(v.String()))
}
