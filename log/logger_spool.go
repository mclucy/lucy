package log

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// maxSpoolBytes bounds one retained process log. A subprocess that prints more
// keeps its start; the spool records the truncation instead of filling the
// user's disk.
const maxSpoolBytes = 64 << 20

// ProcessSpool retains the combined output of the subprocesses one command
// runs. The file appears in the platform log directory under "installer" when
// the first output arrives, so a command that runs no subprocess writes
// nothing. The command boundary retains the file when a step fails and
// discards it when the command succeeds, so a surviving file is always
// failure evidence. Writes are safe from concurrent copy goroutines, which the
// process package starts for stdout and stderr separately.
type ProcessSpool struct {
	mu        sync.Mutex
	prefix    string
	logger    *Logger
	file      *os.File
	path      string
	written   int64
	seen      int64
	capped    bool
	createErr bool
	retained  bool
}

// NewProcessSpool prepares a spool that names its file after prefix. No file
// exists until the first output arrives. A nil logger records no diagnostics
// of the spool itself.
func NewProcessSpool(prefix string, logger *Logger) *ProcessSpool {
	return &ProcessSpool{prefix: prefix, logger: logger}
}

// Write counts every byte and stores what the size bound allows. A spool that
// cannot create or write its file keeps counting and never fails the command;
// the retained byte count then reports how much evidence was lost.
func (s *ProcessSpool) Write(data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen += int64(len(data))
	if !s.ensure() {
		return len(data), nil
	}
	if s.capped {
		return len(data), nil
	}
	remaining := maxSpoolBytes - s.written
	if int64(len(data)) > remaining {
		data = data[:remaining]
	}
	written, err := s.file.Write(data)
	s.written += int64(written)
	if err != nil {
		s.warnOnce(fmt.Errorf("write process log %q: %w", s.path, err))
		return len(data), nil
	}
	if s.written >= maxSpoolBytes {
		s.capped = true
		message := fmt.Sprintf("\n[process output truncated at %d bytes]\n", maxSpoolBytes)
		if _, err := s.file.WriteString(message); err == nil {
			s.written += int64(len(message))
		}
	}
	return len(data), nil
}

// Section introduces one subprocess run in the retained file. It stores the
// label only; the live output writer already narrates to the terminal.
func (s *ProcessSpool) Section(label string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ensure() {
		return
	}
	separator := ""
	if s.written > 0 {
		separator = "\n"
	}
	message := fmt.Sprintf("%s=== %s\n", separator, label)
	written, err := s.file.WriteString(message)
	s.written += int64(written)
	if err != nil {
		s.warnOnce(fmt.Errorf("write process log %q: %w", s.path, err))
	}
}

// Retain closes the file and reports where the evidence survives. It reports
// no path when no output ever arrived, so a failure without subprocess output
// names no file.
func (s *ProcessSpool) Retain() (path string, seen int64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retained = true
	if s.file == nil {
		return "", s.seen, false
	}
	err := s.file.Close()
	s.file = nil
	if err != nil {
		s.warnOnce(fmt.Errorf("close process log %q: %w", s.path, err))
	}
	return s.path, s.seen, true
}

// Discard deletes the unretained file. A spool that a later step writes again
// creates a fresh file, so an action that releases its terminal mid-run loses
// no evidence from the steps that follow.
func (s *ProcessSpool) Discard() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retained {
		return
	}
	if s.file != nil {
		err := errors.Join(s.file.Close(), os.Remove(s.path))
		s.file = nil
		if err != nil {
			s.warnOnce(fmt.Errorf("discard process log %q: %w", s.path, err))
		}
	}
	s.path = ""
	s.written = 0
	s.seen = 0
	s.capped = false
	s.createErr = false
}

// ensure creates the file on first use. It reports whether writes may
// continue; a creation failure is permanent for this spool so a repeated
// warning never replaces the output it reports.
func (s *ProcessSpool) ensure() bool {
	if s.file != nil {
		return true
	}
	if s.createErr {
		return false
	}
	directory := filepath.Join(DefaultDirectory(), "installer")
	name := fmt.Sprintf(
		"%s-%s-%d.log",
		s.prefix,
		time.Now().Format("20060102-150405"),
		os.Getpid(),
	)
	path := filepath.Join(directory, name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		s.warnOnce(fmt.Errorf("create process log directory: %w", err))
		s.createErr = true
		return false
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		s.warnOnce(fmt.Errorf("open process log %q: %w", path, err))
		s.createErr = true
		return false
	}
	s.file = file
	s.path = path
	return true
}

func (s *ProcessSpool) warnOnce(err error) {
	if s.logger != nil {
		s.logger.Warn(err)
	}
}
