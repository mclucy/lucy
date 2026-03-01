// Package logger provides structured logging with clear separation between
// log-file entries (operational diagnostics) and user-facing messages
// (displayed on stderr).
//
// # Function sets
//
// There are three tiers of logging functions plus a fatal shortcut:
//
//	File-only      Info  Warn  Error  Debug   → written to log file; echoed on console only in verboseWrite mode
//	User-display   ShowInfo  ShowWarn  ShowError   → printed to stderr for the user; NOT persisted to log file
//	Both           ReportInfo  ReportWarn  ReportError → written to log file AND printed to stderr
//	Fatal          Fatal   → logged + displayed + os.Exit(1)
//
// All writes to the log file are synchronous (no queue). A history buffer
// records every file-written entry so that [DumpHistory] can replay them to
// the console at program exit for post-mortem inspection.
package logger

import (
	"fmt"
	"os"
	"time"

	"github.com/mclucy/lucy/tools"
)

// Logging only functions

// Info logs an informational entry to the log file.
// In verboseWrite mode the entry is also printed to the console.
func Info(content any) {
	e := &entry{Time: time.Now(), Level: LevelInfo, Content: content}
	record(e)
	writeToFile(e)
	if verboseWrite && LevelInfo >= VerboseLevel {
		writeToConsole(e)
	}
}

// Warn logs a warning to the log file.
// In verboseWrite mode the entry is also printed to the console.
func Warn(content error) {
	if content == nil {
		return
	}
	e := &entry{Time: time.Now(), Level: LevelWarn, Content: content}
	record(e)
	writeToFile(e)
	if verboseWrite && LevelWarn >= VerboseLevel {
		writeToConsole(e)
	}
}

// Error logs an error to the log file.
// In verboseWrite mode the entry is also printed to the console.
func Error(content error) {
	if content == nil {
		return
	}
	e := &entry{Time: time.Now(), Level: LevelError, Content: content}
	record(e)
	writeToFile(e)
	if verboseWrite && LevelError >= VerboseLevel {
		writeToConsole(e)
	}
}

// Debug logs a debug entry to the log file. No-op unless debug mode is on.
// In verboseWrite mode (implied by debug) the entry is also printed to the console.
func Debug(content any) {
	if !debug {
		return
	}
	e := &entry{Time: time.Now(), Level: LevelDebug, Content: content}
	record(e)
	writeToFile(e)
	if verboseWrite {
		writeToConsole(e)
	}
}

// User-display only functions

// ShowInfo displays an informational message to the user on stderr.
// The message is NOT written to the log file.
func ShowInfo(content any) {
	writeToConsole(&entry{Time: time.Now(), Level: LevelInfo, Content: content})
}

// ShowWarn displays a warning to the user on stderr.
// The message is NOT written to the log file.
func ShowWarn(content error) {
	writeToConsole(&entry{Time: time.Now(), Level: LevelWarn, Content: content})
}

// ShowError displays an error to the user on stderr.
// The message is NOT written to the log file.
func ShowError(content error) {
	writeToConsole(
		&entry{
			Time: time.Now(), Level: LevelError, Content: content,
		},
	)
}

// Both file and user-display functions

// ReportInfo logs an informational message to the file AND displays it to
// the user on stderr.
func ReportInfo(content any) {
	e := &entry{Time: time.Now(), Level: LevelInfo, Content: content}
	record(e)
	writeToFile(e)
	writeToConsole(e)
}

// ReportWarn logs a warning to the file AND displays it to the user on
// stderr.
func ReportWarn(content error) {
	if content == nil {
		return
	}
	e := &entry{Time: time.Now(), Level: LevelWarn, Content: content}
	record(e)
	writeToFile(e)
	writeToConsole(e)
}

// ReportError logs an error to the file AND displays it to the user on
// stderr.
func ReportError(content error) {
	if content == nil {
		return
	}
	e := &entry{Time: time.Now(), Level: LevelError, Content: content}
	record(e)
	writeToFile(e)
	writeToConsole(e)
}

// Fatal logs a fatal error to the file, displays it to the user, then

// Fatal logs a fatal error to the file, displays it to the user, then
// calls os.Exit(1). Pending history is dumped before exit.
func Fatal(content error) {
	e := &entry{Time: time.Now(), Level: LevelFatal, Content: content}
	record(e)
	writeToFile(e)
	writeToConsole(e)
	DumpHistory()
	os.Exit(1)
}

// DumpHistory replays all recorded log entries to the console. This is
// intended to be called from a deferred function in main for post-mortem
// inspection in verboseWrite/debug mode.
//
// Entries already shown via verboseWrite mode will appear again — this is
// intentional so that the dump provides a complete, uninterrupted
// chronological view.
func DumpHistory() {
	if !dumpHistory || len(history) == 0 {
		return
	}
	_, _ = fmt.Fprintln(os.Stderr)
	_, _ = fmt.Fprintln(
		os.Stderr,
		tools.Dim("── Log history ("+LogFile.Name()+") ──"),
	)
	for _, e := range history {
		timestamp := tools.Dim(e.Time.Format("15:04:05"))
		_, _ = fmt.Fprintln(
			os.Stderr,
			timestamp,
			e.Level.prefix(true),
			e.Content,
		)
	}
}
