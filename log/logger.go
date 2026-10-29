// Package log records operational diagnostics independently of user reports.
package log

import (
	"fmt"
	"io"
	"os"
	"sync"

	charm "charm.land/log/v2"
	"github.com/charmbracelet/colorprofile"
)

// Logger owns one diagnostic destination and its optional terminal mirror.
// A nil Logger records nothing.
type Logger struct {
	mu         sync.RWMutex
	file       *os.File
	path       string
	diagnostic *charm.Logger
	mirror     *charm.Logger
}

// New records all diagnostic levels in path. The level selects which records
// reach mirror; a nil mirror leaves diagnostics in the file only.
func New(path, level string, mirror io.Writer) (*Logger, error) {
	threshold, err := charm.ParseLevel(level)
	if err != nil {
		return nil, fmt.Errorf("log level: %w", err)
	}
	if path == "" {
		path = DefaultFile()
	}
	file, err := openLogFile(path)
	if err != nil {
		return nil, err
	}
	logger := &Logger{
		file:       file,
		path:       path,
		diagnostic: newLogger(file, charm.DebugLevel),
	}
	if mirror != nil {
		logger.mirror = newLogger(mirror, threshold)
	}
	return logger, nil
}

func newLogger(destination io.Writer, level charm.Level) *charm.Logger {
	logger := charm.NewWithOptions(destination, charm.Options{
		ReportTimestamp: true,
		TimeFormat:      "2006-01-02 15:04:05",
		Level:           level,
		Formatter:       charm.TextFormatter,
	})
	logger.SetColorProfile(colorprofile.NoTTY)
	return logger
}

func (l *Logger) Info(content any, fields ...any) {
	l.write(charm.InfoLevel, content, fields...)
}

func (l *Logger) Debug(content any, fields ...any) {
	l.write(charm.DebugLevel, content, fields...)
}

func (l *Logger) Warn(err error) {
	if err != nil {
		l.write(charm.WarnLevel, err)
	}
}

func (l *Logger) Error(err error) {
	if err != nil {
		l.write(charm.ErrorLevel, err)
	}
}

func (l *Logger) write(level charm.Level, content any, fields ...any) {
	if l == nil {
		return
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	l.diagnostic.Log(level, content, fields...)
	if l.mirror != nil {
		l.mirror.Log(level, content, fields...)
	}
}

func (l *Logger) FilePath() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	l.diagnostic.SetOutput(io.Discard)
	if l.mirror != nil {
		l.mirror.SetOutput(io.Discard)
	}
	err := l.file.Close()
	l.file = nil
	if err != nil {
		return fmt.Errorf("close log file %q: %w", l.path, err)
	}
	return nil
}
