// Package log records operational diagnostics in the log file.
// User-facing messages belong to package report.
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
func Info(content any) {
	e := &entry{Time: time.Now(), Level: log.InfoLevel, Content: content}
	record(e)
	getFileLog().Info(fmt.Sprint(content))
	if verboseWrite {
		consoleLog.Info(fmt.Sprint(content))
	}
}

// Warn logs a warning to the log file.
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

// Fatal records a fatal error without terminating the process.
func Fatal(content error) {
	if content == nil {
		return
	}
	e := &entry{Time: time.Now(), Level: log.FatalLevel, Content: content}
	record(e)
	getFileLog().Log(log.FatalLevel, content.Error())
	if verboseWrite {
		consoleLog.Log(log.FatalLevel, content.Error())
	}
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
