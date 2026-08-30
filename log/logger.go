// Package log provides structured logging with clear separation between
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
// Logging is backed by charmbracelet/log. A history buffer records every
// file-written entry so that [DumpHistory] can replay them to the console
// at program exit for post-mortem inspection.
package log

import (
	"fmt"
	"os"
	"time"

	"charm.land/log/v2"

	"github.com/mclucy/lucy/terminal/style"
)

// ── File-only functions ─────────────────────────────────────────────────

// Info logs an informational entry to the log file.
// In verboseWrite mode the entry is also printed to the console.
func Info(content any) {
	e := &entry{Time: time.Now(), Level: log.InfoLevel, Content: content}
	record(e)
	getFileLog().Info(fmt.Sprint(content))
	if verboseWrite {
		consoleLog.Info(fmt.Sprint(content))
	}
}

// Warn logs a warning to the log file.
// In verboseWrite mode the entry is also printed to the console.
func Warn(content error) {
	if content == nil {
		return
	}
	e := &entry{Time: time.Now(), Level: log.WarnLevel, Content: content}
	record(e)
	getFileLog().Warn(content.Error())
	if verboseWrite {
		consoleLog.Warn(content.Error())
	}
}

// Error logs an error to the log file.
// In verboseWrite mode the entry is also printed to the console.
func Error(content error) {
	if content == nil {
		return
	}
	e := &entry{Time: time.Now(), Level: log.ErrorLevel, Content: content}
	record(e)
	getFileLog().Error(content.Error())
	if verboseWrite {
		consoleLog.Error(content.Error())
	}
}

// Debug logs a debug entry to the log file. No-op unless debug mode is on.
// In verboseWrite mode the entry is also printed to the console.
func Debug(content any) {
	if !debug {
		return
	}
	e := &entry{Time: time.Now(), Level: log.DebugLevel, Content: content}
	record(e)
	getFileLog().Debug(fmt.Sprint(content))
	if verboseWrite {
		consoleLog.Debug(fmt.Sprint(content))
	}
}

// ── User-display only functions ─────────────────────────────────────────

// ShowInfo displays an informational message to the user on stderr.
// The message is NOT written to the log file.
func ShowInfo(content any) {
	consoleLog.Info(fmt.Sprint(content))
}

// ShowWarn displays a warning to the user on stderr.
// The message is NOT written to the log file.
func ShowWarn(content error) {
	consoleLog.Warn(content.Error())
}

// ShowError displays an error to the user on stderr.
// The message is NOT written to the log file.
func ShowError(content error) {
	consoleLog.Error(content.Error())
}

// ── Both file and user-display functions ────────────────────────────────

// ReportInfo logs an informational message to the file AND displays it to
// the user on stderr.
func ReportInfo(content any) {
	e := &entry{Time: time.Now(), Level: log.InfoLevel, Content: content}
	record(e)
	msg := fmt.Sprint(content)
	getFileLog().Info(msg)
	consoleLog.Info(msg)
}

// ReportWarn logs a warning to the file AND displays it to the user on
// stderr.
func ReportWarn(content error) {
	if content == nil {
		return
	}
	e := &entry{Time: time.Now(), Level: log.WarnLevel, Content: content}
	record(e)
	getFileLog().Warn(content.Error())
	consoleLog.Warn(content.Error())
}

// ReportError logs an error to the file AND displays it to the user on
// stderr.
func ReportError(content error) {
	if content == nil {
		return
	}
	e := &entry{Time: time.Now(), Level: log.ErrorLevel, Content: content}
	record(e)
	getFileLog().Error(content.Error())
	consoleLog.Error(content.Error())
}

// ── Fatal ───────────────────────────────────────────────────────────────

// Fatal logs a fatal error to the file, displays it to the user, then
// calls os.Exit(1). Pending history is dumped before exit.
func Fatal(content error) {
	e := &entry{Time: time.Now(), Level: log.FatalLevel, Content: content}
	record(e)
	getFileLog().Error(content.Error()) // write to file at error level (Fatal would exit)
	consoleLog.Error(content.Error())   // show to user
	DumpHistory()
	os.Exit(1)
}

// ── History ─────────────────────────────────────────────────────────────

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
		style.Muted("── Log history ("+getLogFile().Name()+") ──"),
	)

	// Create a temporary log for replay with time-only timestamps.
	replay := log.NewWithOptions(
		os.Stderr, log.Options{
			ReportTimestamp: true,
			TimeFormat:      "15:04:05",
			Level:           log.DebugLevel,
		},
	)
	for _, e := range history {
		replay.SetTimeFunction(func(_ time.Time) time.Time { return e.Time })
		replay.Log(e.Level, fmt.Sprint(e.Content))
	}
}
