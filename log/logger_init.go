package log

import (
	"image/color"
	"io"
	"os"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/charmbracelet/colorprofile"
)

// This file contains initialization and global state for the log package.

var (
	debug        bool // when true, Debug() entries are recorded
	verboseWrite bool // when true, file-only entries are also printed to console
	dumpHistory  bool // when true, DumpHistory() will print the history to console
)

var (
	mu      sync.Mutex // write lock for history
	history []*entry
)

// fileLog writes to the log file with timestamps, no color.
// Initialized lazily on first use.
var fileLog *log.Logger

// consoleLog writes styled output to stderr, no timestamps.
var consoleLog *log.Logger

func init() {
	consoleLog = log.NewWithOptions(
		os.Stderr, log.Options{
			ReportTimestamp: false,
			Level:           log.InfoLevel,
		},
	)
	consoleLog.SetStyles(themedStyles())

	// fileLog is initialized lazily via getFileLog() since the log file
	// may not be available at init time.
}

// getFileLog returns the file log, creating it lazily.
func getFileLog() *log.Logger {
	if fileLog == nil {
		f := getLogFile()
		fileLog = log.NewWithOptions(
			f, log.Options{
				ReportTimestamp: true,
				TimeFormat:      "2006-01-02 15:04:05",
				Level:           log.DebugLevel,
				Formatter:       log.TextFormatter,
			},
		)
		fileLog.SetColorProfile(colorprofile.NoTTY)
	}
	return fileLog
}

// EnablePrintLogs enables echoing of file-only log entries to the console.
func EnablePrintLogs() { verboseWrite = true }

// EnableDebug enables Debug-level logging
func EnableDebug() {
	debug = true
	consoleLog.SetLevel(log.DebugLevel)
}

// EnableDumpHistory enables history dump on exit.
func EnableDumpHistory() { dumpHistory = true }

// SetConsoleOutput changes the console log's output writer.
// Useful for testing or redirecting user-facing output.
func SetConsoleOutput(w io.Writer) {
	consoleLog.SetOutput(w)
}

// themedStyles returns charm/log Styles with level colors matching the
// application theme (tui/style). The old hand-rolled log used:
//
//	Debug → Cyan, Info → Green, Warn → Yellow, Error → Red, Fatal → Red
//
// We reproduce these using basic ANSI colors so they stay consistent
// with the semantic roles defined in tui/style (Note, Success, Warning,
// Failure).
func themedStyles() *log.Styles {
	levelStyle := func(name string, fg color.Color) lipgloss.Style {
		return lipgloss.NewStyle().
			SetString(strings.ToUpper(name)).
			Bold(true).
			MaxWidth(4).
			Foreground(fg)
	}

	s := log.DefaultStyles()
	s.Levels[log.DebugLevel] = levelStyle(
		log.DebugLevel.String(),
		lipgloss.Cyan,
	)
	s.Levels[log.InfoLevel] = levelStyle(log.InfoLevel.String(), lipgloss.Green)
	s.Levels[log.WarnLevel] = levelStyle(
		log.WarnLevel.String(),
		lipgloss.Yellow,
	)
	s.Levels[log.ErrorLevel] = levelStyle(log.ErrorLevel.String(), lipgloss.Red)
	s.Levels[log.FatalLevel] = levelStyle(log.FatalLevel.String(), lipgloss.Red)
	return s
}

// TurnOffStyles disables all coloring on the console log. Call this
// alongside style.TurnOffStyles() to keep log output consistent with
// the rest of the UI when --no-style is active.
func TurnOffStyles() {
	consoleLog.SetColorProfile(colorprofile.NoTTY)
}
