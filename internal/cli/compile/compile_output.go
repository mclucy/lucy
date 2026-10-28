package compile

import (
	"errors"
	"strings"
	"sync"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/report"
)

// excerptLines bounds the tail of a failed tool quoted on the console. Git and
// Gradle both explain themselves at the end of their output.
const excerptLines = 20

// processLog captures the raw stdout and stderr of one external tool.
//
// Raw tool output is DEBUG diagnostics. A failed step reports its recent
// output even when diagnostic logging filters out DEBUG records.
//
// os/exec copies a child's stdout and stderr on separate goroutines, so every
// method is guarded.
type processLog struct {
	tool     string
	logger   *log.Logger
	reporter *report.Reporter

	mu      sync.Mutex
	partial string
	recent  []string
}

func newProcessLog(
	tool string,
	logger *log.Logger,
	reporter *report.Reporter,
) *processLog {
	return &processLog{tool: tool, logger: logger, reporter: reporter}
}

// Write forwards a raw chunk, splitting complete lines off for the log file
// and the excerpt while the trailing partial line waits for its terminator.
func (l *processLog) Write(chunk []byte) (int, error) {
	l.consume(chunk)
	return len(chunk), nil
}

// flush records a trailing line that arrived without a newline terminator.
func (l *processLog) flush() {
	l.consume(nil)
}

func (l *processLog) consume(chunk []byte) {
	l.mu.Lock()
	l.partial += string(chunk)
	lines := strings.Split(l.partial, "\n")
	l.partial = lines[len(lines)-1]
	l.recent = append(l.recent, lines[:len(lines)-1]...)
	if len(l.recent) > excerptLines {
		l.recent = l.recent[len(l.recent)-excerptLines:]
	}
	complete := lines[:len(lines)-1]
	l.mu.Unlock()

	if len(complete) > 0 {
		l.logger.Debug("external tool output", "tool", l.tool, "lines", complete)
	}
}

// excerpt quotes the most recent non-blank lines of tool output, or nothing
// when the tool stayed silent.
func (l *processLog) excerpt() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	lines := make([]string, 0, len(l.recent))
	for _, line := range l.recent {
		if trimmed := strings.TrimRight(line, "\r"); strings.TrimSpace(trimmed) != "" {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n" + strings.Join(lines, "\n")
}

// reportFailure shows the tool's own account of a failure and returns err
// unchanged. The excerpt is its own block rather than part of the error, where
// a run of raw lines would be reflowed into a single paragraph.
func reportFailure(output *processLog, err error) error {
	if excerpt := output.excerpt(); excerpt != "" {
		output.reporter.Error(
			errors.New(output.tool + " reported:\n" + indent(excerpt)),
		)
	}
	return err
}

// indent prefixes every line so a block of quoted tool output reads as one.
func indent(block string) string {
	lines := strings.Split(strings.Trim(block, "\n"), "\n")
	for i, line := range lines {
		lines[i] = "  " + line
	}
	return strings.Join(lines, "\n")
}
