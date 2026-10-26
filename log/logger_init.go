package log

import (
	"fmt"
	"io"
	"os"
	"sync"

	"charm.land/log/v2"
	"github.com/charmbracelet/colorprofile"
)

var (
	mu         sync.RWMutex
	output     = &fileOutput{path: DefaultFile()}
	diagnostic = newLogger(output, log.DebugLevel)
	mirror     *log.Logger
)

func newLogger(destination io.Writer, level log.Level) *log.Logger {
	logger := log.NewWithOptions(destination, log.Options{
		ReportTimestamp: true,
		TimeFormat:      "2006-01-02 15:04:05",
		Level:           level,
		Formatter:       log.TextFormatter,
	})
	logger.SetColorProfile(colorprofile.NoTTY)
	return logger
}

// Configure opens the diagnostic destination and, when printLogs is set,
// mirrors records to stderr at the requested level. The file always records
// debug: the flag selects what a person watches, not what survives a failure.
func Configure(path, level string, printLogs bool) error {
	threshold, err := log.ParseLevel(level)
	if err != nil {
		return fmt.Errorf("log level: %w", err)
	}
	if path == "" {
		path = DefaultFile()
	}
	file, err := openLogFile(path)
	if err != nil {
		return err
	}

	mu.Lock()
	defer mu.Unlock()
	previous := output
	output = &fileOutput{path: path, file: file}
	sessionCapture.reset()
	diagnostic = newLogger(io.MultiWriter(&sessionCapture, output), log.DebugLevel)
	mirror = nil
	if printLogs {
		mirror = newLogger(os.Stderr, threshold)
	}
	if err := previous.close(); err != nil {
		return fmt.Errorf("close previous log file: %w", err)
	}
	return nil
}

// FilePath reports where diagnostics are written.
func FilePath() string {
	mu.RLock()
	defer mu.RUnlock()
	return output.path
}

// Close stops diagnostic writes and releases the log file.
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	diagnostic.SetOutput(io.Discard)
	if mirror != nil {
		mirror.SetOutput(io.Discard)
		mirror = nil
	}
	if err := output.close(); err != nil {
		return fmt.Errorf("close log file %q: %w", output.path, err)
	}
	return nil
}
