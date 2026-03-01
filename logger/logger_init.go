package logger

import "sync"

// This file contains initialization and global state for the logger package.

func init() {
	// TODO: This is only for development. In production, this variable will
	// be read from a config file or environment variable.
	VerboseLevel = LevelDebug
}

var (
	debug        bool // when true, Debug() entries are recorded
	verboseWrite bool // when true, file-only entries are also printed to console
	dumpHistory  bool // when true, DumpHistory() will print the history to console
)

var (
	mu      sync.Mutex // write lock for history
	history []*entry
)

// VerboseLevel controls which levels are echoed to the console in verboseWrite
// mode. Everything at or above this level is shown. Set to LevelDebug so
// that all entries are visible.
var VerboseLevel Level

// EnablePrintLogs enables echoing of file-only log entries to the console.
func EnablePrintLogs() { verboseWrite = true }

// EnableDebug enables Debug-level logging
func EnableDebug() { debug = true }

func EnableDumpHistory() { dumpHistory = true }
