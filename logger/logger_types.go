package logger

import (
	"time"

	"github.com/mclucy/lucy/tools"
)

// Level represents the severity of a log entry.
// Levels are ordered from least to most severe: Debug < Info < Warn < Error < Fatal.
type Level uint8

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// levelColor maps each level to a styling function.
var levelColor = map[Level]func(any) string{
	LevelDebug: tools.Green,
	LevelInfo:  tools.Cyan,
	LevelWarn:  tools.Yellow,
	LevelError: tools.Red,
	LevelFatal: tools.Red,
}

// prefix returns the bracketed level tag, optionally colored.
func (l Level) prefix(colored bool) string {
	if colored {
		return "[" + levelColor[l](l.String()) + "]"
	}
	return "[" + l.String() + "]"
}

// Entry represents a single log item with its timestamp, level, and content. This is used internally for recording history and is not exposed to users of the logger package.
type entry struct {
	Time    time.Time
	Level   Level
	Content any
}
