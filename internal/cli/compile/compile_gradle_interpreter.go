package compile

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// fallbackShell interprets Gradle wrappers that declare no usable shebang. It
// is the shell a wrapper without a shebang would expect anyway.
const fallbackShell = "/bin/sh"

// wrapperInterpreter returns the shell to run a Gradle wrapper that Lucy
// cannot execute directly. Gradle ships a bash script before 8.2 and a POSIX
// one from 8.2 on, so the shebang is the only reliable statement of which
// dialect the wrapper is written in; the fallback shell rejects the bash one
// wherever it is dash. Reading it here is a fallback, not the usual path: an
// executable wrapper is started directly and lets the system read it instead.
func wrapperInterpreter(script string) string {
	if shell, ok := shebangShell(script); ok {
		return shell
	}
	return fallbackShell
}

// shebangShell resolves the interpreter a script's shebang line names, and
// reports whether the script declares one that is present on this machine.
func shebangShell(script string) (string, bool) {
	line, err := readFirstLine(script)
	if err != nil {
		return "", false
	}
	line, declared := strings.CutPrefix(line, "#!")
	if !declared {
		return "", false
	}
	fields := strings.Fields(line)
	if len(fields) > 0 && filepath.Base(fields[0]) == "env" {
		fields = fields[1:]
	}
	for _, field := range fields {
		if strings.HasPrefix(field, "-") {
			continue
		}
		resolved, err := exec.LookPath(field)
		return resolved, err == nil
	}
	return "", false
}

// readFirstLine returns the first line of a script without its line ending.
func readFirstLine(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	line, err := bufio.NewReader(file).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
