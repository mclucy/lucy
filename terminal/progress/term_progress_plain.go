package progress

import (
	"io"
	"strings"
	"sync"
	"time"

	"github.com/mclucy/lucy/terminal/style"
)

const plainHeartbeat = 10 * time.Second

// plain writes append-only lines. It suits pipes, logs, and CI, where a
// redrawn area would only add noise.
type plain struct {
	out io.Writer

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

func newPlain(out io.Writer) *plain {
	p := &plain{out: out, stop: make(chan struct{}), done: make(chan struct{})}
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
			if !p.closed && p.status != "" && p.status != p.lastStatus {
				p.lastStatus = p.status
				p.lastPrint = time.Now()
				line := "  " + p.status + p.elapsedSuffix()
				if p.phase != "" {
					line = "  " + p.phase + ": " + p.status + p.elapsedSuffix()
				}
				p.mu.Unlock()
				p.println(line)
				continue
			}
			p.mu.Unlock()
		}
	}
}

func (p *plain) elapsedSuffix() string {
	if p.phaseStart.IsZero() {
		return ""
	}
	return style.Muted(" (" + FormatElapsed(time.Since(p.phaseStart)) + ")")
}

func (p *plain) PhaseStarted(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.phase = text
	p.phaseStart = time.Now()
	p.status = ""
	p.lastStatus = ""
}

func (p *plain) PhaseDone(text string, elapsed time.Duration) {
	p.mu.Lock()
	p.phase = ""
	p.phaseStart = time.Time{}
	p.status = ""
	p.lastStatus = ""
	p.mu.Unlock()
	p.println(phaseLine(text, elapsed))
}

func (p *plain) Status(text string) {
	p.mu.Lock()
	p.status = text
	p.mu.Unlock()
}

func (p *plain) Download(string, int64, int64) {}

func (p *plain) DownloadDone() {}

func (p *plain) Println(lines ...string) {
	for _, line := range lines {
		p.println(line)
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

func (p *plain) println(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
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
		_, _ = io.WriteString(w.renderer.out, style.Muted("  │ "+line)+"\n")
	}
	return len(data), nil
}
