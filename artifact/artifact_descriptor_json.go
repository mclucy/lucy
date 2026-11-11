package artifact

import (
	"encoding/json/v2"
	"fmt"
	"io"
)

// readDescriptorJSON decodes a descriptor the way its loader does. Published
// descriptors sometimes embed raw control characters in string values; strict
// JSON rejects them, but the loader accepts the file, so identification must
// not fail on them.
func readDescriptorJSON(reader io.Reader, value any) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	return unmarshalDescriptor(data, value)
}

// unmarshalDescriptor decodes descriptor bytes with the same tolerance.
func unmarshalDescriptor(raw []byte, value any) error {
	return json.Unmarshal(escapeJSONControlCharacters(raw), value)
}

// escapeJSONControlCharacters rewrites raw control characters inside JSON
// string literals as escape sequences. Bytes outside strings and escaped
// characters are copied unchanged.
func escapeJSONControlCharacters(raw []byte) []byte {
	escaped := make([]byte, 0, len(raw))
	inString := false
	afterBackslash := false
	for _, b := range raw {
		if !inString {
			if b == '"' {
				inString = true
			}
			escaped = append(escaped, b)
			continue
		}
		if afterBackslash {
			afterBackslash = false
			escaped = append(escaped, b)
			continue
		}
		switch b {
		case '\\':
			afterBackslash = true
			escaped = append(escaped, b)
		case '"':
			inString = false
			escaped = append(escaped, b)
		case '\n':
			escaped = append(escaped, '\\', 'n')
		case '\r':
			escaped = append(escaped, '\\', 'r')
		case '\t':
			escaped = append(escaped, '\\', 't')
		default:
			if b < 0x20 {
				escaped = append(escaped, fmt.Sprintf("\\u%04x", b)...)
				continue
			}
			escaped = append(escaped, b)
		}
	}
	return escaped
}
