package telemetry

import (
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/mclucy/lucy/log"
)

const pathMarker = "<path>"

var (
	authorizationPattern = regexp.MustCompile(
		`(?i)\b(authorization\s*[:=]\s*)(?:bearer\s+)?[^\s"',;]+`,
	)
	bearerPattern = regexp.MustCompile(`(?i)\b(bearer\s+)[A-Za-z0-9._~+/=-]+`)
	secretPattern = regexp.MustCompile(
		`(?i)\b([a-z0-9_.-]*(?:token|secret|password|passwd|api[_-]?key|apikey|credential|authorization)[a-z0-9_.-]*)(["']?\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;&"'<>]+)`,
	)
	jsonSecretPattern = regexp.MustCompile(
		`(?i)("(?:[^"]*(?:token|secret|password|apikey|api_key|authorization)[^"]*)"\s*:\s*)"[^"]*"`,
	)
	queryPattern     = regexp.MustCompile(`([?&][^=&\s"'<>]+=)[^&#\s"'<>]*`)
	userInfoPattern  = regexp.MustCompile(`(?i)(https?://)[^/@\s]+@`)
	drivePathPattern = regexp.MustCompile(`(?i)\b[a-z]:\\[^\s"'<>]+`)
	uncPathPattern   = regexp.MustCompile(`\\\\[^\s"'<>]+`)
	unixHomePattern  = regexp.MustCompile(`/(?:home|Users|root|tmp)/[^/\s"'<>]+(?:/[^\s"'<>]*)?`)
)

// sanitizer holds the local values that must never leave the machine.
type sanitizer struct {
	token      string
	names      []string
	paths      []string
	genericWin bool
}

func newSanitizer(token string) sanitizer {
	value := sanitizer{token: token, genericWin: runtime.GOOS == "windows"}
	if current, err := user.Current(); err == nil {
		value.names = append(value.names, current.Username)
		if index := strings.LastIndexAny(current.Username, `\/`); index >= 0 {
			value.names = append(value.names, current.Username[index+1:])
		}
	}
	for _, name := range []string{"USERNAME", "USER"} {
		if raw := os.Getenv(name); raw != "" {
			value.names = append(value.names, raw)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		value.paths = append(value.paths, home)
		value.names = append(value.names, filepath.Base(home))
	}
	if directory, err := os.Getwd(); err == nil {
		value.paths = append(value.paths, directory)
	}
	if root, err := stateRoot(); err == nil {
		value.paths = append(value.paths, root)
	}
	value.paths = append(value.paths, os.TempDir(), log.DefaultFile())
	value.paths = compactStrings(value.paths)
	return value
}

func compactStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		return len(result[i]) > len(result[j])
	})
	return result
}

// sanitize removes recognized credentials, query values, user names, and
// local absolute paths. It is best effort and preserves diagnostic structure.
func (s sanitizer) sanitize(content string) string {
	if content == "" {
		return ""
	}
	if len(s.token) >= 4 {
		content = strings.ReplaceAll(content, s.token, "<redacted>")
	}
	content = replacePath(content, s.paths, s.genericWin)
	content = authorizationPattern.ReplaceAllString(content, "$1<redacted>")
	content = bearerPattern.ReplaceAllString(content, "$1<redacted>")
	content = jsonSecretPattern.ReplaceAllString(content, "$1\"<redacted>\"")
	content = secretPattern.ReplaceAllString(content, "$1$2<redacted>")
	content = queryPattern.ReplaceAllString(content, "$1<redacted>")
	content = userInfoPattern.ReplaceAllString(content, "$1<redacted>@")
	content = drivePathPattern.ReplaceAllString(content, pathMarker)
	content = uncPathPattern.ReplaceAllString(content, pathMarker)
	content = unixHomePattern.ReplaceAllString(content, pathMarker)
	for _, name := range s.names {
		if len(name) < 3 {
			continue
		}
		pattern := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\b`)
		content = pattern.ReplaceAllString(content, "<user>")
	}
	return content
}

func replacePath(content string, paths []string, caseInsensitive bool) string {
	for _, path := range paths {
		if len(path) < 3 {
			continue
		}
		expression := regexp.QuoteMeta(path)
		prefix := ""
		if caseInsensitive {
			prefix = "(?i)"
		}
		pattern := regexp.MustCompile(prefix + expression + `[^\s"'<>]*`)
		content = pattern.ReplaceAllString(content, pathMarker)
	}
	return content
}

// sanitizeError bounds a failure description after filtering.
func (s sanitizer) sanitizeError(content string) string {
	content = strings.TrimSpace(s.sanitize(content))
	if content == "" {
		return ""
	}
	if len(content) <= maxErrorBytes {
		return content
	}
	return string(truncateHead([]byte(content), maxErrorBytes))
}

// sanitizeLog keeps the trailing bytes so the end of a failure survives.
func (s sanitizer) sanitizeLog(content string) (string, bool) {
	if content == "" {
		return "", false
	}
	filtered := []byte(s.sanitize(content))
	tail, truncated := truncateTail(filtered, maxLogBytes)
	return string(tail), truncated
}

func truncateTail(content []byte, limit int) ([]byte, bool) {
	if len(content) <= limit {
		return content, false
	}
	tail := content[len(content)-limit:]
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
	}
	for len(tail) > 0 && !utf8.Valid(tail) {
		tail = tail[1:]
	}
	return tail, true
}

func truncateHead(content []byte, limit int) []byte {
	if len(content) <= limit {
		return content
	}
	head := content[:limit]
	for len(head) > 0 && !utf8.Valid(head) {
		head = head[:len(head)-1]
	}
	return head
}
