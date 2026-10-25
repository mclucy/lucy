// Package shell formats text that a POSIX shell reads back.
package shell

import "strings"

// Quote returns s safe to paste into a POSIX shell command. Plain words stay
// bare so quoted output stays readable; anything a shell would interpret is
// single-quoted.
func Quote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`!*?[]{}()<>|&;#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
