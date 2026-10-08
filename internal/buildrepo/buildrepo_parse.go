package buildrepo

import (
	"regexp"
	"strings"
)

var (
	callPattern     = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	identifierWord  = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
	assignmentValue = regexp.MustCompile(`=[ \t]*([^\n]*)`)
)

func calls(content, name string) []int {
	mask := stringMask(content)
	var out []int
	for _, match := range callPattern.FindAllStringSubmatchIndex(content, -1) {
		if content[match[2]:match[3]] != name || inLiteral(mask, match[0]) {
			continue
		}
		paren := strings.IndexByte(content[match[3]:], '(')
		if paren < 0 {
			continue
		}
		out = append(out, match[3]+paren)
	}
	return out
}

// groovyCalls returns the offsets of the first argument of every Groovy style
// call to name, which passes string literals without parentheses.
func groovyCalls(content, name string) []int {
	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `[ \t\r\n]+['"]`)
	var out []int
	for _, loc := range pattern.FindAllStringIndex(content, -1) {
		out = append(out, loc[1]-1)
	}
	return out
}

// callArgs returns the comma separated arguments of a call starting at at,
// which is either an opening paren or the first argument of a Groovy style
// call, together with the line of the call.
func callArgs(content string, at int) ([]string, int) {
	line := lineAt(content, at)
	if at < len(content) && content[at] == '(' {
		body, ok := balancedParens(content, at)
		if !ok || strings.TrimSpace(body) == "" {
			return nil, line
		}
		return splitArgs(body), line
	}
	var args []string
	for index := at; index < len(content); {
		for index < len(content) && (isSpace(content[index]) || content[index] == ',') {
			index++
		}
		if index >= len(content) {
			break
		}
		_, width, ok := readStringLiteral(content[index:])
		if !ok {
			break
		}
		args = append(args, content[index:index+width])
		index += width
	}
	return args, line
}

// balancedParens returns the content between the paren at start and its match.
func balancedParens(content string, start int) (string, bool) {
	depth := 0
	for i := start; i < len(content); i++ {
		switch content[i] {
		case '\'', '"':
			end := skipString(content, i)
			if end < 0 {
				return "", false
			}
			i = end
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return content[start+1 : i], true
			}
		case '{':
			// A named argument block never closes with the call's paren.
			if !isNamedArg(content[start:i]) {
				return "", false
			}
		}
	}
	return "", false
}

// isNamedArg reports whether text before a brace looks like "name =".
func isNamedArg(text string) bool {
	trimmed := strings.TrimSpace(text)
	if !strings.HasSuffix(trimmed, "=") {
		return false
	}
	trimmed = strings.TrimSuffix(trimmed, "=")
	if strings.TrimSpace(trimmed) == trimmed {
		return false
	}
	return identifierWord.MatchString(trimmed)
}

func skipString(content string, start int) int {
	quote := content[start]
	for i := start + 1; i < len(content); i++ {
		switch content[i] {
		case '\\':
			i++
		case quote:
			return i
		}
	}
	return -1
}

// splitArgs splits a call body on top level commas.
func splitArgs(body string) []string {
	var args []string
	depth, start := 0, 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case '\'', '"':
			end := skipString(body, i)
			if end < 0 {
				return args
			}
			i = end
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				args = append(args, strings.TrimSpace(body[start:i]))
				start = i + 1
			}
		}
	}
	if last := strings.TrimSpace(body[start:]); last != "" {
		args = append(args, last)
	}
	return args
}

func assignmentAfter(content string, paren int, name string) (string, bool) {
	_, ok := balancedParens(content, paren)
	if !ok {
		return "", false
	}
	end := closingParen(content, paren)
	rest := content[end+1:]
	trimmed := strings.TrimLeft(rest, " \t\r\n")
	if strings.HasPrefix(trimmed, "{") {
		body, _, ok := block(trimmed, 0)
		if !ok {
			return "", false
		}
		for _, entry := range splitEntries(body) {
			key, value, found := strings.Cut(entry, "=")
			if found && strings.TrimSpace(key) == name {
				return strings.TrimSpace(value), true
			}
		}
		return "", false
	}
	if !strings.HasPrefix(trimmed, "."+name) {
		return "", false
	}
	trimmed = strings.TrimLeft(trimmed[len(name)+1:], " \t\r\n")
	if !strings.HasPrefix(trimmed, "=") || strings.HasPrefix(trimmed, "==") {
		return "", false
	}
	match := assignmentValue.FindStringSubmatch(trimmed)
	if match == nil {
		return "", false
	}
	return strings.TrimSpace(match[1]), true
}

func unwrapValue(text string) (string, bool) {
	text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), ";"))
	if value, ok := literalString(text); ok {
		return value, true
	}
	open := strings.IndexByte(text, '(')
	if open < 0 || closingParen(text, open) != len(text)-1 {
		return "", false
	}
	callee := strings.TrimSpace(text[:open])
	body, ok := balancedParens(text, open)
	if !ok {
		return "", false
	}
	arguments := splitArgs(body)
	switch callee {
	case "file", "project.file", "rootProject.file", "rootDir.resolve", "settingsDir.resolve":
		if len(arguments) == 1 {
			return literalString(arguments[0])
		}
	case "File", "new File":
		if len(arguments) == 1 {
			return literalString(arguments[0])
		}
		if len(arguments) == 2 && (arguments[0] == "rootDir" || arguments[0] == "settingsDir") {
			return literalString(arguments[1])
		}
	}
	return "", false
}

func closingParen(content string, start int) int {
	depth := 0
	for i := start; i < len(content); i++ {
		switch content[i] {
		case '\'', '"':
			end := skipString(content, i)
			if end < 0 {
				return start
			}
			i = end
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return start
}

// literalString unwraps a literal that spans all of text.
func literalString(text string) (string, bool) {
	text = strings.TrimSpace(text)
	value, width, ok := readStringLiteral(text)
	if !ok || width != len(text) {
		return "", false
	}
	return value, true
}

// readStringLiteral reads a quoted string at the start of text and reports how
// many bytes it consumed, including both quotes.
func readStringLiteral(text string) (string, int, bool) {
	if len(text) < 2 {
		return "", 0, false
	}
	quote := text[0]
	if quote != '\'' && quote != '"' {
		return "", 0, false
	}
	for i := 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case '\n':
			return "", 0, false
		case quote:
			return text[1:i], i + 1, true
		}
	}
	return "", 0, false
}

// block returns the body of the brace block whose opening brace is at offset.
func block(content string, offset int) (string, int, bool) {
	if offset >= len(content) || content[offset] != '{' {
		return "", offset, false
	}
	depth := 0
	for i := offset; i < len(content); i++ {
		switch content[i] {
		case '\'', '"':
			end := skipString(content, i)
			if end < 0 {
				return "", offset, false
			}
			i = end
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return content[offset+1 : i], offset + i + 1, true
			}
		}
	}
	return "", offset, false
}

// blockNamed returns the body of the first "name {" block found in content
// together with the offset of the body's first byte.
func blockNamed(content, name string) (string, int, bool) {
	from := 0
	for {
		index := strings.Index(content[from:], name)
		if index < 0 {
			return "", 0, false
		}
		start := from + index
		rest := content[start+len(name):]
		trimmed := strings.TrimLeft(rest, " \t\r\n")
		brace := start + len(name) + len(rest) - len(trimmed)
		if strings.HasPrefix(trimmed, "{") {
			body, _, ok := block(content, brace)
			return body, brace + 1, ok
		}
		from = start + len(name)
	}
}

// stripComments blanks out comments while preserving string literals and line
// structure, so commented out declarations are never mistaken for applied
// ones and reported line numbers stay correct.
func stripComments(content string) string {
	out := []byte(content)
	for i := 0; i < len(content); i++ {
		switch content[i] {
		case '\'', '"':
			end := skipString(content, i)
			if end < 0 {
				return string(out)
			}
			i = end
		case '/':
			if i+1 >= len(content) {
				continue
			}
			switch content[i+1] {
			case '/':
				for ; i < len(content) && content[i] != '\n'; i++ {
					out[i] = ' '
				}
			case '*':
				for ; i+1 < len(content) && !(content[i] == '*' && content[i+1] == '/'); i++ {
					if content[i] != '\n' {
						out[i] = ' '
					}
				}
			}
		}
	}
	return string(out)
}

// stringMask marks every byte that lies inside a string literal, so that text
// such as "build.release.platform" is never read as a Gradle declaration.
func stringMask(content string) []bool {
	mask := make([]bool, len(content))
	var quote byte
	for i := 0; i < len(content); i++ {
		switch {
		case quote == 0 && (content[i] == '\'' || content[i] == '"'):
			quote = content[i]
		case quote != 0 && content[i] == '\\':
			i++
		case quote != 0 && content[i] == quote:
			quote = 0
		case quote != 0:
			mask[i] = true
		}
	}
	return mask
}

// inLiteral reports whether the byte at pos lies inside a string literal.
func inLiteral(mask []bool, pos int) bool {
	return pos >= 0 && pos < len(mask) && mask[pos]
}
