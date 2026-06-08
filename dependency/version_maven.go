package dependency

import (
	"strings"

	"github.com/mclucy/lucy/types"
)

// MavenVersion implements Maven-style version ordering for Forge/NeoForge
// mod metadata and Maven version ranges.
type MavenVersion struct {
	original string
	tokens   []mavenToken
}

type mavenToken struct {
	separator byte
	value     string
	numeric   bool
}

func parseMavenVersion(raw types.BareVersion) types.ResolvableVersion {
	original := strings.TrimSpace(string(raw))
	if original == "" {
		return nil
	}
	return &MavenVersion{
		original: original,
		tokens:   tokenizeMavenVersion(original),
	}
}

func (v *MavenVersion) Compare(other types.ResolvableVersion) (int, bool) {
	if v == nil || other == nil {
		return 0, false
	}

	var o *MavenVersion
	switch other := other.(type) {
	case *MavenVersion:
		o = other
	case *SemverVersion:
		parsed := parseMavenVersion(types.BareVersion(other.String()))
		var ok bool
		o, ok = parsed.(*MavenVersion)
		if !ok {
			return 0, false
		}
	default:
		return 0, false
	}
	if o == nil {
		return 0, false
	}

	maxLen := len(v.tokens)
	if len(o.tokens) > maxLen {
		maxLen = len(o.tokens)
	}
	for i := 0; i < maxLen; i++ {
		left := mavenTokenAt(v.tokens, i, mavenTokenSeparatorAt(o.tokens, i))
		right := mavenTokenAt(o.tokens, i, mavenTokenSeparatorAt(v.tokens, i))
		if cmp := compareMavenToken(left, right); cmp != 0 {
			return cmp, true
		}
	}
	return 0, true
}

func (v *MavenVersion) Validate() bool {
	return v != nil && v.original != ""
}

func (v *MavenVersion) Scheme() types.VersionScheme {
	return types.Maven
}

func (v *MavenVersion) String() string {
	if v == nil {
		return ""
	}
	return v.original
}

func tokenizeMavenVersion(raw string) []mavenToken {
	tokens := make([]mavenToken, 0)
	separator := byte('.')
	var buf strings.Builder
	previousClass := byte(0)

	flush := func() {
		if buf.Len() == 0 {
			return
		}
		value := buf.String()
		tokens = append(tokens, mavenToken{
			separator: separator,
			value:     value,
			numeric:   isMavenNumeric(value),
		})
		buf.Reset()
	}

	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if isMavenSeparator(ch) {
			flush()
			separator = ch
			previousClass = 0
			continue
		}

		class := mavenCharClass(ch)
		if buf.Len() > 0 && previousClass != 0 && class != previousClass {
			flush()
			separator = '-'
		}
		buf.WriteByte(ch)
		previousClass = class
	}
	flush()

	return trimMavenNullTokens(tokens)
}

func isMavenSeparator(ch byte) bool {
	return ch == '.' || ch == '-' || ch == '_'
}

func mavenCharClass(ch byte) byte {
	if ch >= '0' && ch <= '9' {
		return '0'
	}
	return 'a'
}

func isMavenNumeric(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func trimMavenNullTokens(tokens []mavenToken) []mavenToken {
	tokens = trimTrailingMavenNullTokens(tokens)
	for i := len(tokens) - 1; i >= 0; i-- {
		if tokens[i].separator != '-' {
			continue
		}
		start := i
		for start > 0 && isMavenNullToken(tokens[start-1]) {
			start--
		}
		if start == i {
			continue
		}
		tokens = append(tokens[:start], tokens[i:]...)
		i = start
	}
	return trimTrailingMavenNullTokens(tokens)
}

func trimTrailingMavenNullTokens(tokens []mavenToken) []mavenToken {
	for len(tokens) > 0 && isMavenNullToken(tokens[len(tokens)-1]) {
		tokens = tokens[:len(tokens)-1]
	}
	return tokens
}

func isMavenNullToken(token mavenToken) bool {
	if token.numeric {
		return compareMavenInteger(token.value, "0") == 0
	}
	switch normalizeMavenQualifier(token.value) {
	case "", "final", "ga", "release":
		return true
	default:
		return false
	}
}

func mavenTokenAt(tokens []mavenToken, index int, otherSeparator byte) mavenToken {
	if index < len(tokens) {
		return tokens[index]
	}
	if otherSeparator == 0 {
		otherSeparator = '.'
	}
	if otherSeparator == '.' {
		return mavenToken{separator: otherSeparator, value: "0", numeric: true}
	}
	return mavenToken{separator: otherSeparator, value: "", numeric: false}
}

func mavenTokenSeparatorAt(tokens []mavenToken, index int) byte {
	if index < len(tokens) {
		return tokens[index].separator
	}
	return 0
}

func compareMavenToken(left, right mavenToken) int {
	if left.separator != right.separator {
		if cmp := compareInt(mavenTokenKind(left), mavenTokenKind(right)); cmp != 0 {
			return cmp
		}
	}

	if left.numeric && right.numeric {
		return compareMavenInteger(left.value, right.value)
	}
	if !left.numeric && !right.numeric {
		return compareMavenQualifier(left.value, right.value)
	}
	if left.numeric {
		return 1
	}
	return -1
}

func mavenTokenKind(token mavenToken) int {
	if !token.numeric {
		return 0
	}
	if token.separator == '.' {
		return 2
	}
	return 1
}

func compareMavenInteger(left, right string) int {
	left = strings.TrimLeft(left, "0")
	right = strings.TrimLeft(right, "0")
	if left == "" {
		left = "0"
	}
	if right == "" {
		right = "0"
	}
	if cmp := compareInt(len(left), len(right)); cmp != 0 {
		return cmp
	}
	return strings.Compare(left, right)
}

func compareMavenQualifier(left, right string) int {
	left = normalizeMavenQualifier(left)
	right = normalizeMavenQualifier(right)
	leftRank, leftKnown := mavenQualifierRank(left)
	rightRank, rightKnown := mavenQualifierRank(right)
	if leftKnown || rightKnown {
		if cmp := compareInt(leftRank, rightRank); cmp != 0 {
			return cmp
		}
		if leftKnown && rightKnown {
			return 0
		}
	}
	return strings.Compare(left, right)
}

func normalizeMavenQualifier(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func mavenQualifierRank(value string) (int, bool) {
	switch value {
	case "alpha", "a":
		return 0, true
	case "beta", "b":
		return 1, true
	case "milestone", "m":
		return 2, true
	case "rc", "cr":
		return 3, true
	case "snapshot":
		return 4, true
	case "", "final", "ga", "release":
		return 5, true
	case "sp":
		return 6, true
	default:
		return 7, false
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
