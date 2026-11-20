package progress

import (
	"io"
	"strings"
	"sync"
	"time"

	"github.com/mclucy/lucy/terminal/style"
)

const (
	plainHeartbeat = 10 * time.Second
	// plainStatusRepeat bounds how often an unchanged status line repeats.
	// A long phase without counter updates still shows elapsed progress, but a
	// silent minute does not flood an append-only log.
	plainStatusRepeat = 30 * time.Second
)

// plain writes append-only lines. It suits pipes, logs, and CI, where a
// redrawn area would only add noise.
type plain struct {
	out   io.Writer
	theme style.Theme

	mu         sync.Mutex
	phase      string
	phaseStart time.Time
	status     string
	lastStatus string
	lastPrint  time.Time
	closed     bool
	stop       chan struct{}
	done       chan struct{}
}

func newPlain(out io.Writer, theme style.Theme) *plain {
	p := &plain{out: out, theme: theme, stop: make(chan struct{}), done: make(chan struct{})}
	go p.heartbeat()
	return p
}

func (p *plain) heartbeat() {
	defer close(p.done)
	ticker := time.NewTicker(plainHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			p.mu.Lock()
			if p.closed || p.phase == "" {
				p.mu.Unlock()
				continue
			}
			now := time.Now()
			// A status that changed prints at the next heartbeat. An
			// unchanged one repeats only after plainStatusRepeat, which
			// still advances elapsed time for a silent phase.
			if p.status == p.lastStatus && now.Sub(p.lastPrint) < plainStatusRepeat {
				p.mu.Unlock()
				continue
			}
			p.lastStatus = p.status
			p.lastPrint = now
			// The line is written while the state lock is held: PhaseDone
			// clears the phase under the same lock and prints the completion
			// under it, so a heartbeat can never land after its phase ended.
			line := "  " + p.phase + p.elapsedSuffix(now)
			if p.status != "" {
				line = "  " + p.phase + ": " + p.status + p.elapsedSuffix(now)
			}
			p.writeLineLocked(line)
			p.mu.Unlock()
		}
	}
}

func (p *plain) elapsedSuffix(now time.Time) string {
	if p.phaseStart.IsZero() {
		return ""
	}
	return p.theme.Muted(" (" + FormatElapsed(now.Sub(p.phaseStart)) + ")")
}

func (p *plain) PhaseStarted(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.phase = text
	p.phaseStart = time.Now()
	p.status = ""
	p.lastStatus = ""
	p.lastPrint = p.phaseStart
	if text != "" {
		p.writeLineLocked(phaseStartLine(text, p.theme))
	}
}

func (p *plain) PhaseDone(text string, elapsed time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.phase = ""
	p.phaseStart = time.Time{}
	p.status = ""
	p.lastStatus = ""
	// An empty text ends the phase without a completion line.
	if text != "" {
		p.writeLineLocked(phaseLine(text, elapsed, p.theme))
	}
}

func (p *plain) Status(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.status = text
}

func (p *plain) Download(string, int64, int64) {}

func (p *plain) DownloadDone() {}

func (p *plain) Println(lines ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	for _, line := range lines {
		p.writeLineLocked(line)
	}
}

func (p *plain) LogWriter() io.Writer { return plainLogWriter{p} }

func (p *plain) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	p.mu.Unlock()
	close(p.stop)
	<-p.done
}

func (p *plain) writeLineLocked(line string) {
	if line == "" {
		_, _ = io.WriteString(p.out, "\n")
		return
	}
	_, _ = io.WriteString(p.out, line+"\n")
}

type plainLogWriter struct{ renderer *plain }

func (w plainLogWriter) Write(data []byte) (int, error) {
	w.renderer.mu.Lock()
	defer w.renderer.mu.Unlock()
	if w.renderer.closed {
		return io.Discard.Write(data)
	}
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		_, _ = io.WriteString(w.renderer.out, w.renderer.theme.Muted("  │ "+line)+"\n")
	}
	return len(data), nil
}
