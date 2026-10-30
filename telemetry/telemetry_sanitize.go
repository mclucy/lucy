package telemetry

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	bearerPattern         = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/\-=]+`)
	authorizationPattern  = regexp.MustCompile(`(?i)\b([a-z0-9_-]*authorization)\b\s*[:=]\s*[^\r\n]+`)
	flagCredentialPattern = regexp.MustCompile(`(?i)(--?(?:token|password|passwd|secret|api[_-]?key|access[_-]?key))(\s+|=)("(?:\\.|[^"\\])*"?|'(?:\\.|[^'\\])*'?|[^\s]+)`)
	credentialPattern     = regexp.MustCompile(`(?i)\b([a-z0-9_]*(?:api[_-]?key|access[_-]?key|private[_-]?key|secret|token|password|passwd|pwd|authorization|cookie|session[_-]?id)[a-z0-9_]*)\b["']?\s*[:=]\s*("(?:\\.|[^"\\])*"?|'(?:\\.|[^'\\])*'?|[^\s,;]+)`)
	commonTokenPattern    = regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{16,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|xox[baprs]-[A-Za-z0-9-]{10,})\b`)
	urlCredentialPattern  = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@`)
	privateKeyPattern     = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|$)`)
	ipv4Pattern           = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	ipv6Candidate         = regexp.MustCompile(`\b[0-9A-Fa-f:]{2,39}\b`)
	unixPathPattern       = regexp.MustCompile(`(^|[\s"'(\\[=:,<])(/(?:[A-Za-z0-9._-]+/)+[A-Za-z0-9._-]+)`)
	homePathPattern       = regexp.MustCompile(`~/[^\s"'<>]+`)
	windowsPathPattern    = regexp.MustCompile(`(?i)\b[A-Z]:\\[^\s"'<>]+`)
	uncPathPattern        = regexp.MustCompile(`\\\\[A-Za-z0-9._-]+\\[^\s"'<>]+`)
)

// sanitizeText converts free-form text into bounded, printable UTF-8. It
// normalizes line endings, removes ANSI escape sequences and control or
// formatting characters other than newline and tab, replaces invalid UTF-8,
// and appends a truncation marker when the byte bound is exceeded.
func sanitizeText(raw string, maxBytes int) (string, bool) {
	if raw == "" || maxBytes <= 0 {
		return "", false
	}
	truncated := false
	if maxBytes <= (1<<30)/8 && len(raw) > maxBytes*8 {
		raw = truncateUTF8(raw, maxBytes*8)
		truncated = true
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	var builder strings.Builder
	builder.Grow(min(len(raw), maxBytes))
	for index := 0; index < len(raw); {
		character, size := utf8.DecodeRuneInString(raw[index:])
		if character == utf8.RuneError && size == 1 {
			builder.WriteRune(utf8.RuneError)
			index++
			continue
		}
		index += size
		switch {
		case character == '\x1b':
			index = skipEscapeSequence(raw, index)
		case character == '\n' || character == '\t':
			builder.WriteRune(character)
		case character < 0x20 || character == 0x7f:
			continue
		case character >= 0x80 && character <= 0x9f:
			continue
		case unicode.Is(unicode.Cf, character):
			continue
		default:
			builder.WriteRune(character)
		}
	}
	text := strings.TrimSpace(builder.String())
	if len(text) <= maxBytes {
		return text, truncated
	}
	if maxBytes <= len(truncationMarker) {
		return truncateUTF8(text, maxBytes), true
	}
	return truncateUTF8(text, maxBytes-len(truncationMarker)) + truncationMarker, true
}

func skipEscapeSequence(raw string, index int) int {
	if index >= len(raw) {
		return index
	}
	switch raw[index] {
	case '[':
		index++
		for index < len(raw) {
			character := raw[index]
			index++
			if character >= 0x40 && character <= 0x7e {
				break
			}
		}
		return index
	case ']', 'P', '^', '_':
		index++
		for index < len(raw) {
			if raw[index] == 0x07 {
				return index + 1
			}
			if raw[index] == 0x1b && index+1 < len(raw) && raw[index+1] == '\\' {
				return index + 2
			}
			index++
		}
		return index
	default:
		return index + 1
	}
}

func truncateUTF8(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	cut := value[:maxBytes]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

func sanitizeToken(raw string, maxBytes int) string {
	var builder strings.Builder
	for _, character := range raw {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case strings.ContainsRune("-_.:", character):
		default:
			continue
		}
		builder.WriteRune(character)
		if builder.Len() >= maxBytes {
			break
		}
	}
	return builder.String()
}

func validChannel(channel string) bool {
	if channel == "" || len(channel) > 32 {
		return false
	}
	for index, character := range channel {
		valid := character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9'
		if index > 0 {
			valid = valid || character == '-' || character == '_' || character == '.'
		}
		if !valid {
			return false
		}
	}
	return true
}

func normalizeCommand(raw string) (string, error) {
	if len(raw) > 512 {
		return "", ErrInvalidEvent
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 || len(fields) > 4 {
		return "", ErrInvalidEvent
	}
	var builder strings.Builder
	for index, field := range fields {
		if len(field) > 32 || !validCommandToken(field) {
			return "", ErrInvalidEvent
		}
		if index > 0 {
			builder.WriteByte(' ')
		}
		builder.WriteString(field)
	}
	command := builder.String()
	if len(command) > 96 {
		return "", ErrInvalidEvent
	}
	return command, nil
}

func validCommandToken(token string) bool {
	for index := 0; index < len(token); index++ {
		character := token[index]
		valid := character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9'
		if index > 0 {
			valid = valid || character == '-' || character == '_' || character == ':'
		}
		if !valid {
			return false
		}
	}
	return true
}

func validCode(raw string, maxBytes int) bool {
	if raw == "" || len(raw) > maxBytes {
		return false
	}
	for index := 0; index < len(raw); index++ {
		character := raw[index]
		valid := character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9'
		if index > 0 {
			valid = valid || character == '-' || character == '_' || character == '.' || character == ':'
		}
		if !valid {
			return false
		}
	}
	return true
}

func sanitizeErrorText(err error) string {
	if err == nil {
		return ""
	}
	text, _ := sanitizeText(err.Error(), 256)
	return strings.Join(strings.Fields(text), " ")
}

// sanitizeReportText applies the printable-text filter and then redacts common
// secrets, network addresses, and absolute paths. Redaction is defense in
// depth, not anonymization: callers must still keep raw transcripts and file
// attachments out of reports.
func sanitizeReportText(raw string, maxBytes int) (string, bool) {
	if raw == "" || maxBytes <= 0 {
		return "", false
	}
	inputLimit := maxBytes * 8
	truncated := len(raw) > inputLimit
	if truncated {
		raw = truncateUTF8(raw, inputLimit)
	}
	text, inputClipped := sanitizeText(raw, inputLimit)
	truncated = truncated || inputClipped
	if text == "" {
		return "", truncated
	}
	text = redactReportText(text)
	bounded, clipped := boundWithMarker(text, maxBytes)
	return bounded, truncated || clipped
}

func redactReportText(text string) string {
	text = privateKeyPattern.ReplaceAllString(text, "[redacted-private-key]")
	text = bearerPattern.ReplaceAllString(text, "Bearer [redacted]")
	text = authorizationPattern.ReplaceAllString(text, "${1}=[redacted]")
	text = flagCredentialPattern.ReplaceAllString(text, "${1}${2}[redacted]")
	text = credentialPattern.ReplaceAllString(text, "${1}=[redacted]")
	text = commonTokenPattern.ReplaceAllString(text, "[redacted-token]")
	text = urlCredentialPattern.ReplaceAllString(text, "${1}[redacted]@")
	text = redactIPv4(text)
	text = redactIPv6(text)
	text = windowsPathPattern.ReplaceAllString(text, "[redacted-path]")
	text = uncPathPattern.ReplaceAllString(text, "[redacted-path]")
	text = homePathPattern.ReplaceAllString(text, "[redacted-path]")
	text = unixPathPattern.ReplaceAllString(text, "${1}[redacted-path]")
	return text
}

func redactIPv4(text string) string {
	return ipv4Pattern.ReplaceAllStringFunc(text, func(candidate string) string {
		for _, part := range strings.Split(candidate, ".") {
			value, err := strconv.Atoi(part)
			if err != nil || value > 255 {
				return candidate
			}
		}
		return "[redacted-ip]"
	})
}

func redactIPv6(text string) string {
	return ipv6Candidate.ReplaceAllStringFunc(text, func(candidate string) string {
		if !strings.Contains(candidate, ":") {
			return candidate
		}
		if strings.Contains(candidate, "::") {
			return "[redacted-ip]"
		}
		groups := 0
		for _, group := range strings.Split(candidate, ":") {
			if group != "" {
				groups++
			}
		}
		if groups >= 4 && strings.Count(candidate, ":") >= 3 {
			return "[redacted-ip]"
		}
		return candidate
	})
}

func boundWithMarker(text string, maxBytes int) (string, bool) {
	if len(text) <= maxBytes {
		return text, false
	}
	text = strings.TrimSuffix(text, truncationMarker)
	if maxBytes <= len(truncationMarker) {
		return truncateUTF8(text, maxBytes), true
	}
	return truncateUTF8(text, maxBytes-len(truncationMarker)) + truncationMarker, true
}
