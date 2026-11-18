package log

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// maxSpoolBytes bounds one retained process log. A subprocess that prints more
// keeps its start; the spool records the truncation instead of filling the
// user's disk.
const maxSpoolBytes = 64 << 20

// truncationMarker ends a file that omitted output. Room for it is reserved
// from maxSpoolBytes, so the file never exceeds the bound.
const truncationMarker = "\n[process output truncated]\n"

// contentLimit bounds headers and subprocess output, leaving the marker its
// space inside maxSpoolBytes.
const contentLimit int64 = maxSpoolBytes - int64(len(truncationMarker))

// ProcessLog reports what became of a retained process log. Observed counts
// every byte the subprocesses printed; Stored counts the bytes the file
// holds, including command headers. Truncated reports that the file is
// incomplete because the size bound was reached or a write failed.
type ProcessLog struct {
	Path      string
	Observed  int64
	Stored    int64
	Truncated bool
}

// ProcessSpool retains the combined output of the subprocesses one command
// runs. The file appears in the platform log directory under "installer" when
// the first subprocess is about to start, because its command header is
// written before the child runs. The command boundary retains the file when a
// step fails and discards it when the command succeeds, so a surviving file is
// always failure evidence. Writes are safe from concurrent copy goroutines,
// which the process package starts for stdout and stderr separately.
type ProcessSpool struct {
	mu          sync.Mutex
	prefix      string
	logger      *Logger
	file        *os.File
	path        string
	written     int64
	seen        int64
	capped      bool
	createErr   bool
	writeErr    bool
	retained    bool
	retainedLog ProcessLog
	closed      bool
}

// NewProcessSpool prepares a spool that names its file after prefix. No file
// exists until the first subprocess is about to start. A nil logger records no
// diagnostics of the spool itself.
func NewProcessSpool(prefix string, logger *Logger) *ProcessSpool {
	return &ProcessSpool{prefix: prefix, logger: logger}
}

// Write counts every byte and stores what the size bound allows. It always
// reports the full input length: the copy goroutines of the process package
// read a short write as a broken pipe and would fail the command over its
// diagnostic record. A spool that cannot create or write its file keeps
// counting and never fails the command; the retained log then reports how
// much evidence was lost. Output that arrives after Retain or Discard is
// accepted and dropped, so the retained file stays the evidence it named.
func (s *ProcessSpool) Write(data []byte) (int, error) {
	total := len(data)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retained || s.closed {
		return total, nil
	}
	s.seen += int64(total)
	if s.writeErr || !s.ensure() {
		return total, nil
	}
	s.append(data)
	return total, nil
}

// Section introduces one subprocess run in the retained file. It stores the
// label only; the live output writer already narrates to the terminal.
func (s *ProcessSpool) Section(label string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retained || s.closed || s.writeErr || !s.ensure() {
		return
	}
	separator := ""
	if s.written > 0 {
		separator = "\n"
	}
	s.append([]byte(fmt.Sprintf("%s=== %s\n", separator, label)))
}

// Retain closes the file and reports where the evidence survives. Retention is
// terminal: a repeated call reports the same log, and no later write reopens
// the file. It reports no log when no file was created, so a failure without
// a subprocess run names no file.
func (s *ProcessSpool) Retain() (ProcessLog, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retained {
		return s.retainedLog, s.retainedLog.Path != ""
	}
	s.retained = true
	if s.file == nil {
		return ProcessLog{}, false
	}
	err := s.file.Close()
	s.file = nil
	if err != nil {
		s.warnOnce(fmt.Errorf("close process log %q: %w", s.path, err))
	}
	s.retainedLog = ProcessLog{
		Path:      s.path,
		Observed:  s.seen,
		Stored:    s.written,
		Truncated: s.capped || s.writeErr || err != nil,
	}
	return s.retainedLog, true
}

// Discard deletes the unretained file and ends the spool, so no later write
// leaves a file the command boundary does not own.
func (s *ProcessSpool) Discard() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retained || s.closed {
		return
	}
	s.closed = true
	if s.file == nil {
		return
	}
	err := errors.Join(s.file.Close(), os.Remove(s.path))
	s.file = nil
	if err != nil {
		s.warnOnce(fmt.Errorf("discard process log %q: %w", s.path, err))
	}
}

// append stores what fits of data and, when it must drop bytes, ends the file
// with the truncation marker. Headers and subprocess output share the bound,
// so neither can push the file past it.
func (s *ProcessSpool) append(data []byte) {
	if s.capped || s.writeErr {
		return
	}
	room := contentLimit - s.written
	if room < 0 {
		room = 0
	}
	if int64(len(data)) <= room {
		s.store(data)
		return
	}
	if room > 0 {
		s.store(data[:room])
	}
	s.capped = true
	s.store([]byte(truncationMarker))
}

// store writes what the file accepts and counts it. The spool is a diagnostic
// record; a file that stopped accepting writes must never fail or spam over
// the command it records.
func (s *ProcessSpool) store(data []byte) {
	if len(data) == 0 {
		return
	}
	written, err := s.file.Write(data)
	s.written += int64(written)
	if err != nil {
		s.writeErr = true
		s.warnOnce(fmt.Errorf("write process log %q: %w", s.path, err))
	}
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
	stamp := time.Now().Format("20060102-150405")
	name := fmt.Sprintf("%s-%s-%d.log", s.prefix, stamp, os.Getpid())
	path := filepath.Join(directory, name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		s.warnOnce(fmt.Errorf("create process log directory: %w", err))
		s.createErr = true
		return false
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		// Another spool of the same second took the timestamped name,
		// so the random suffix keeps this run's evidence.
		file, err = os.CreateTemp(directory, s.prefix+"-"+stamp+"-*.log")
	}
	if err != nil {
		s.warnOnce(fmt.Errorf("open process log %q: %w", path, err))
		s.createErr = true
		return false
	}
	s.file = file
	s.path = file.Name()
	return true
}

func (s *ProcessSpool) warnOnce(err error) {
	if s.logger != nil {
		s.logger.Warn(err)
	}
}
