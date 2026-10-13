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
	diagnostic = log.NewWithOptions(output, log.Options{
		ReportTimestamp: true,
		TimeFormat:      "2006-01-02 15:04:05",
		Level:           log.InfoLevel,
		Formatter:       log.TextFormatter,
	})
)

func init() {
	diagnostic.SetColorProfile(colorprofile.NoTTY)
}

// Configure opens the diagnostic destination before replacing the current stream.
// Mirroring writes the same filtered, formatted records to stderr.
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
	var destination io.Writer = output
	if printLogs {
		destination = io.MultiWriter(output, os.Stderr)
	}
	diagnostic.SetOutput(destination)
	diagnostic.SetLevel(threshold)
	if err := previous.close(); err != nil {
		return fmt.Errorf("close previous log file: %w", err)
	}
	return nil
}

// Close stops diagnostic writes and releases the log file.
func Close() error {
	mu.Lock()
	defer mu.Unlock()
	diagnostic.SetOutput(io.Discard)
	if err := output.close(); err != nil {
		return fmt.Errorf("close log file %q: %w", output.path, err)
	}
	return nil
}
