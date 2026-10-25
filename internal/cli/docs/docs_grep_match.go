package docs

import (
	"regexp"
	"strings"
)

type lineMatcher func(string) bool

func newLineMatcher(patterns []string, opts *grepOptions) (lineMatcher, error) {
	if opts.fixed {
		return newFixedMatcher(patterns, opts), nil
	}
	return newRegexpMatcher(patterns, opts)
}

func newFixedMatcher(patterns []string, opts *grepOptions) lineMatcher {
	needles := make([]string, len(patterns))
	for index, pattern := range patterns {
		if opts.ignoreCase {
			pattern = strings.ToLower(pattern)
		}
		needles[index] = pattern
	}
	return func(line string) bool {
		if opts.ignoreCase {
			line = strings.ToLower(line)
		}
		for _, needle := range needles {
			if fixedLineMatches(line, needle, opts.word, opts.wholeLine) {
				return true
			}
		}
		return false
	}
}

func fixedLineMatches(line, needle string, word, wholeLine bool) bool {
	if needle == "" {
		return !wholeLine || line == ""
	}
	if wholeLine {
		return line == needle
	}
	if !word {
		return strings.Contains(line, needle)
	}
	for start := 0; start <= len(line); {
		index := strings.Index(line[start:], needle)
		if index < 0 {
			return false
		}
		index += start
		before := index == 0 || !isWordByte(line[index-1])
		end := index + len(needle)
		after := end == len(line) || !isWordByte(line[end])
		if before && after {
			return true
		}
		start = index + 1
	}
	return false
}

func isWordByte(value byte) bool {
	return value == '_' ||
		(value >= '0' && value <= '9') ||
		(value >= 'a' && value <= 'z') ||
		(value >= 'A' && value <= 'Z')
}

func newRegexpMatcher(patterns []string, opts *grepOptions) (lineMatcher, error) {
	expressions := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		expression := pattern
		if opts.word {
			expression = `\b(?:` + expression + `)\b`
		}
		if opts.wholeLine {
			expression = `^(?:` + expression + `)$`
		}
		if opts.ignoreCase {
			expression = `(?i)` + expression
		}
		compiled, err := regexp.Compile(expression)
		if err != nil {
			return nil, err
		}
		expressions = append(expressions, compiled)
	}
	return func(line string) bool {
		for _, compiled := range expressions {
			if compiled.MatchString(line) {
				return true
			}
		}
		return false
	}, nil
}
