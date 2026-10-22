package version

import (
	"encoding/json/jsontext"
	"strconv"
	"strings"
)

// SemverVersion implements the semantic grammar shared by Fabric Loader and
// MCDR: one or more numeric components, an optional prerelease, and build
// metadata. Build metadata never participates in comparison.
type SemverVersion struct {
	dialect       Dialect
	numbers       []uint64
	wildcards     []bool
	prerelease    string
	hasPrerelease bool
	original      string
}

// StringVersion is a version outside the semantic grammar. Fabric Loader
// compares it as a plain string; the opaque dialect uses it for identifiers.
type StringVersion struct {
	dialect  Dialect
	original string
}

type semverParseOptions struct {
	dialect       Dialect
	allowWildcard bool
	validateBuild bool
}

func parseSemverVersion(
	raw string,
	options semverParseOptions,
) *SemverVersion {
	core := raw
	if index := strings.IndexByte(core, '+'); index >= 0 {
		build := core[index+1:]
		if options.validateBuild && !validExtraIdentifiers(build, true) {
			return nil
		}
		core = core[:index]
	}

	prerelease := ""
	hasPrerelease := false
	if index := strings.IndexByte(core, '-'); index >= 0 {
		prerelease = core[index+1:]
		hasPrerelease = true
		core = core[:index]
		if !validExtraIdentifiers(prerelease, false) {
			return nil
		}
	}

	if core == "" || strings.HasPrefix(core, ".") || strings.HasSuffix(core, ".") {
		return nil
	}

	parts := strings.Split(core, ".")
	numbers := make([]uint64, 0, len(parts))
	var wildcards []bool
	for _, part := range parts {
		if isWildcardComponent(part) {
			if !options.allowWildcard {
				return nil
			}
			if wildcards == nil {
				wildcards = make([]bool, len(parts))
			}
			wildcards[len(numbers)] = true
			numbers = append(numbers, 0)
			continue
		}
		number, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return nil
		}
		numbers = append(numbers, number)
	}

	return &SemverVersion{
		dialect:       options.dialect,
		numbers:       numbers,
		wildcards:     wildcards,
		prerelease:    prerelease,
		hasPrerelease: hasPrerelease,
		original:      raw,
	}
}

func parseFabricVersion(raw Version) ResolvableVersion {
	text := string(raw)
	if text == "" {
		return nil
	}
	if parsed := parseSemverVersion(
		text,
		semverParseOptions{dialect: DialectFabric},
	); parsed != nil {
		return parsed
	}
	return &StringVersion{
		dialect:  DialectFabric,
		original: text,
	}
}

func parseMCDRVersion(raw string, allowWildcard bool) ResolvableVersion {
	parsed := parseSemverVersion(
		raw,
		semverParseOptions{
			dialect:       DialectMCDR,
			allowWildcard: allowWildcard,
			validateBuild: true,
		},
	)
	if parsed == nil {
		return nil
	}
	return parsed
}

// validExtraIdentifiers accepts prerelease or build identifiers allowed by the
// semantic grammars. An empty value is valid and denotes an empty prerelease.
func validExtraIdentifiers(value string, allowPlus bool) bool {
	if value == "" {
		return true
	}
	for identifier := range strings.SplitSeq(value, ".") {
		if identifier == "" {
			return false
		}
		for _, ch := range identifier {
			if ch == '-' || (allowPlus && ch == '+') ||
				(ch >= '0' && ch <= '9') ||
				(ch >= 'A' && ch <= 'Z') ||
				(ch >= 'a' && ch <= 'z') {
				continue
			}
			return false
		}
	}
	return true
}

// newSemverBoundary creates a version directly below every prerelease and
// above every lower core version, matching Fabric's trailing '-' predicate
// boundary.
func newSemverBoundary(
	dialect Dialect,
	numbers ...uint64,
) *SemverVersion {
	return &SemverVersion{
		dialect:       dialect,
		numbers:       numbers,
		hasPrerelease: true,
		original:      joinSemverNumbers(numbers) + "-",
	}
}

func joinSemverNumbers(numbers []uint64) string {
	parts := make([]string, len(numbers))
	for index, number := range numbers {
		parts[index] = strconv.FormatUint(number, 10)
	}
	return strings.Join(parts, ".")
}

func (s *SemverVersion) component(index int) (uint64, bool) {
	if s == nil || index < 0 {
		return 0, false
	}
	if index < len(s.numbers) {
		return s.numbers[index], s.wildcards != nil && s.wildcards[index]
	}
	if len(s.numbers) > 0 && s.wildcards != nil && s.wildcards[len(s.numbers)-1] {
		return 0, true
	}
	return 0, false
}

func (s *SemverVersion) Major() uint64 {
	value, _ := s.component(0)
	return value
}

func (s *SemverVersion) Minor() uint64 {
	value, _ := s.component(1)
	return value
}

func (s *SemverVersion) Patch() uint64 {
	value, _ := s.component(2)
	return value
}

func (s *SemverVersion) wildcardAt(index int) bool {
	_, wildcard := s.component(index)
	return wildcard
}

func (s *SemverVersion) hasWildcard() bool {
	if s == nil {
		return false
	}
	for _, wildcard := range s.wildcards {
		if wildcard {
			return true
		}
	}
	return false
}

func (s *SemverVersion) Prerelease() string {
	if s == nil {
		return ""
	}
	return s.prerelease
}

func (s *SemverVersion) Dialect() Dialect {
	if s == nil {
		return DialectUnknown
	}
	return s.dialect
}

func (s *SemverVersion) Compare(other ResolvableVersion) (int, bool) {
	if s == nil || other == nil {
		return 0, false
	}
	switch other := other.(type) {
	case *SemverVersion:
		if other.dialect != s.dialect {
			return 0, false
		}
		count := max(len(s.numbers), len(other.numbers))
		for index := range count {
			left, leftWildcard := s.component(index)
			right, rightWildcard := other.component(index)
			if leftWildcard || rightWildcard {
				continue
			}
			if comparison := compareUint(left, right); comparison != 0 {
				return comparison, true
			}
		}
		switch s.dialect {
		case DialectFabric:
			return compareFabricPrerelease(s, other), true
		case DialectMCDR:
			return compareMCDRPrerelease(s, other), true
		default:
			return 0, false
		}
	case *StringVersion:
		if other.dialect != s.dialect {
			return 0, false
		}
		return strings.Compare(s.String(), other.String()), true
	default:
		return 0, false
	}
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
		if comparison := comparePrereleaseIdentifier(
			leftParts[index],
			rightParts[index],
		); comparison != 0 {
			return comparison
		}
	}
	return compareInt(len(leftParts), len(rightParts))
}

// compareMCDRPrerelease follows MCDReforged's _ExtraElement ordering. The whole
// prerelease is one element split at its final dot into a body and an optional
// numeric suffix.
func compareMCDRPrerelease(left, right *SemverVersion) int {
	switch {
	case left.hasPrerelease && right.hasPrerelease:
		return compareMCDRExtra(left.prerelease, right.prerelease)
	case left.hasPrerelease:
		if right.hasWildcard() {
			return 0
		}
		return -1
	case right.hasPrerelease:
		if left.hasWildcard() {
			return 0
		}
		return 1
	default:
		return 0
	}
}

type mcdrExtra struct {
	text   string
	body   string
	number int64
	hasNum bool
}

func parseMCDRExtra(value string) mcdrExtra {
	extra := mcdrExtra{text: value, body: value}
	if index := strings.LastIndexByte(value, '.'); index >= 0 {
		if number, err := strconv.ParseInt(value[index+1:], 10, 64); err == nil {
			extra.body = value[:index]
			extra.number = number
			extra.hasNum = true
		}
	}
	return extra
}

func (e mcdrExtra) String() string {
	if e.hasNum {
		return e.body + "." + strconv.FormatInt(e.number, 10)
	}
	return e.text
}

func (e mcdrExtra) less(other mcdrExtra) bool {
	if !e.hasNum || !other.hasNum {
		return e.String() < other.String()
	}
	if e.body != other.body {
		return e.body < other.body
	}
	return e.number < other.number
}

func compareMCDRExtra(left, right string) int {
	leftExtra := parseMCDRExtra(left)
	rightExtra := parseMCDRExtra(right)
	if leftExtra.less(rightExtra) {
		return -1
	}
	if rightExtra.less(leftExtra) {
		return 1
	}
	return 0
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

func compareInt(left, right int) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func (s *SemverVersion) Validate() bool {
	return s != nil && len(s.numbers) > 0
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

func (v *StringVersion) Dialect() Dialect {
	if v == nil {
		return DialectUnknown
	}
	return v.dialect
}

func (v *StringVersion) Compare(other ResolvableVersion) (int, bool) {
	if v == nil || other == nil || other.Dialect() != v.dialect {
		return 0, false
	}
	switch other := other.(type) {
	case *StringVersion:
		if v.dialect == DialectOpaque {
			if v.original != other.original {
				return 0, false
			}
			return 0, true
		}
		return strings.Compare(v.original, other.original), true
	case *SemverVersion:
		return strings.Compare(v.original, other.String()), true
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
