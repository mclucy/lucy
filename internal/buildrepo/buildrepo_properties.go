package buildrepo

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
)

// ParseProperties decodes Java-properties escapes and continued logical lines.
func ParseProperties(content string) (map[string]string, error) {
	properties := make(map[string]string)
	pending := ""
	parse := func(line string) error {
		line = strings.TrimLeft(line, " \t\f")
		if line == "" || line[0] == '#' || line[0] == '!' {
			return nil
		}
		end := 0
		for end < len(line) {
			if line[end] == '\\' {
				end = min(end+2, len(line))
				continue
			}
			if propertySpace(line[end]) || line[end] == '=' || line[end] == ':' {
				break
			}
			end++
		}
		valueStart := end
		for valueStart < len(line) && propertySpace(line[valueStart]) {
			valueStart++
		}
		if valueStart < len(line) && (line[valueStart] == '=' || line[valueStart] == ':') {
			valueStart++
		}
		for valueStart < len(line) && propertySpace(line[valueStart]) {
			valueStart++
		}
		key, err := decodeProperty(line[:end])
		if err != nil {
			return err
		}
		value, err := decodeProperty(line[valueStart:])
		if err != nil {
			return fmt.Errorf("property %q: %w", key, err)
		}
		properties[key] = value
		return nil
	}
	for raw := range strings.SplitSeq(content, "\n") {
		line := strings.TrimSuffix(raw, "\r")
		if pending != "" {
			line = pending + strings.TrimLeft(line, " \t\f")
		} else if trimmed := strings.TrimLeft(line, " \t\f"); trimmed != "" && (trimmed[0] == '#' || trimmed[0] == '!') {
			// A comment line never continues, even when it ends with a
			// backslash: "# e.g. C:\Tools\" must not swallow the property
			// that follows it.
			continue
		}
		trailing := 0
		for i := len(line) - 1; i >= 0 && line[i] == '\\'; i-- {
			trailing++
		}
		if trailing%2 != 0 {
			pending = line[:len(line)-1]
			continue
		}
		pending = ""
		if err := parse(line); err != nil {
			return nil, err
		}
	}
	if err := parse(pending); err != nil {
		return nil, err
	}
	return properties, nil
}

func propertySpace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\f'
}

func decodeProperty(text string) (string, error) {
	if !strings.ContainsRune(text, '\\') {
		return text, nil
	}
	var decoded strings.Builder
	decoded.Grow(len(text))
	for i := 0; i < len(text); i++ {
		if text[i] != '\\' {
			decoded.WriteByte(text[i])
			continue
		}
		i++
		if i == len(text) {
			return "", fmt.Errorf("incomplete property escape")
		}
		switch text[i] {
		case 't':
			decoded.WriteByte('\t')
		case 'r':
			decoded.WriteByte('\r')
		case 'n':
			decoded.WriteByte('\n')
		case 'f':
			decoded.WriteByte('\f')
		case 'u':
			value, err := propertyUnicode(text[i+1:])
			if err != nil {
				return "", err
			}
			i += 4
			if utf16.IsSurrogate(value) {
				if !strings.HasPrefix(text[i+1:], `\u`) {
					return "", fmt.Errorf("unpaired property unicode surrogate")
				}
				low, err := propertyUnicode(text[i+3:])
				if err != nil {
					return "", err
				}
				value = utf16.DecodeRune(value, low)
				if value == '\uFFFD' {
					return "", fmt.Errorf("invalid property unicode surrogate pair")
				}
				i += 6
			}
			decoded.WriteRune(value)
		default:
			decoded.WriteByte(text[i])
		}
	}
	return decoded.String(), nil
}

func propertyUnicode(text string) (rune, error) {
	if len(text) < 4 {
		return 0, fmt.Errorf("incomplete property unicode escape")
	}
	value, err := strconv.ParseUint(text[:4], 16, 16)
	if err != nil {
		return 0, fmt.Errorf("invalid property unicode escape: %w", err)
	}
	return rune(value), nil
}
