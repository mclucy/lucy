package fileglob

import "strings"

// IsPattern reports whether value contains pattern metacharacters. A caller
// that finds an entry under the literal path skips this check, so a name
// with pattern characters stays addressable.
func IsPattern(value string) bool {
	return strings.ContainsAny(value, "*?[")
}

// globPattern escapes the brace characters. Braced alternation is not part of
// the supported syntax, so braces match only themselves.
func globPattern(pattern string) string {
	return strings.NewReplacer(
		"{", `\{`,
		"}", `\}`,
	).Replace(pattern)
}
