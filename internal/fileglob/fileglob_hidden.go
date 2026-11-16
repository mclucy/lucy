package fileglob

import (
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// explicitlySelectsHidden reports whether pattern selects every hidden
// component of name with an explicit leading dot. The WithNoHidden option of
// doublestar checks file attributes on Windows only, so this rule applies to
// every filesystem and to in-memory entries.
func explicitlySelectsHidden(pattern, name string) bool {
	segments := strings.Split(pattern, "/")
	components := strings.Split(name, "/")
	type position struct{ segment, component int }
	type outcome struct {
		selects bool
		known   bool
	}
	memo := make(map[position]outcome, len(segments)*len(components))
	var search func(segment, component int) bool
	search = func(segment, component int) bool {
		at := position{segment, component}
		if result := memo[at]; result.known {
			return result.selects
		}
		selects := false
		switch {
		case segment == len(segments):
			selects = component == len(components)
		case segments[segment] == "**":
			selects = search(segment+1, component) ||
				component < len(components) &&
					!hiddenComponent(components[component]) &&
					search(segment, component+1)
		case component < len(components):
			matched, err := doublestar.Match(segments[segment], components[component])
			selects = err == nil && matched &&
				(!hiddenComponent(components[component]) ||
					explicitDotSegment(segments[segment])) &&
				search(segment+1, component+1)
		}
		memo[at] = outcome{selects: selects, known: true}
		return selects
	}
	return search(0, 0)
}

// hiddenComponent reports whether a path component starts with a dot.
func hiddenComponent(component string) bool {
	return strings.HasPrefix(component, ".")
}

// explicitDotSegment reports whether a pattern segment selects a leading dot
// through a literal dot, an escaped dot, or a non-negated character class that
// includes one.
func explicitDotSegment(segment string) bool {
	if strings.HasPrefix(segment, ".") {
		return true
	}
	if strings.HasPrefix(segment, `\`) {
		return len(segment) > 1 && segment[1] == '.'
	}
	class := leadingCharacterClass(segment)
	if class == "" {
		return false
	}
	content := class[1 : len(class)-1]
	if strings.HasPrefix(content, "!") || strings.HasPrefix(content, "^") {
		return false
	}
	matched, err := doublestar.Match(class, ".")
	return err == nil && matched
}

// leadingCharacterClass returns the first character class of a pattern
// segment, including its brackets. It returns an empty string when the
// segment does not start with a character class.
func leadingCharacterClass(segment string) string {
	if !strings.HasPrefix(segment, "[") {
		return ""
	}
	index := 1
	if index < len(segment) && (segment[index] == '!' || segment[index] == '^') {
		index++
	}
	if index < len(segment) && segment[index] == ']' {
		index++
	}
	for ; index < len(segment); index++ {
		switch segment[index] {
		case '\\':
			index++
		case ']':
			return segment[:index+1]
		}
	}
	return ""
}
