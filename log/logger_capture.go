package log

import "sync"

// captureLimit bounds the diagnostics one process keeps for its own use.
// Tail readers may still ask for less; the slack exists so filtering can see
// complete secrets before the payload is truncated.
const captureLimit = 2 << 20

// captureBuffer keeps the tail of this process's formatted diagnostic records.
type captureBuffer struct {
	mu       sync.Mutex
	data     []byte
	overflow bool
}

func (b *captureBuffer) Write(record []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, record...)
	if len(b.data) > 2*captureLimit {
		drop := len(b.data) - captureLimit
		b.data = append(b.data[:0], b.data[drop:]...)
		b.overflow = true
	}
	return len(record), nil
}

// Snapshot returns at most limit trailing bytes and reports whether anything
// was dropped before them.
func (b *captureBuffer) Snapshot(limit int) ([]byte, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if limit < 0 {
		limit = 0
	}
	truncated := b.overflow
	start := 0
	if len(b.data) > limit {
		start = len(b.data) - limit
		truncated = true
	}
	out := make([]byte, len(b.data)-start)
	copy(out, b.data[start:])
	return out, truncated
}

func (b *captureBuffer) reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = b.data[:0]
	b.overflow = false
}

var sessionCapture captureBuffer

// Snapshot returns the diagnostic records this process wrote, keeping at most
// limit trailing bytes. The log file may also hold records from older
// processes; this snapshot never does.
func Snapshot(limit int) ([]byte, bool) {
	return sessionCapture.Snapshot(limit)
}
