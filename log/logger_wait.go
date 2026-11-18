package log

import "time"

// WaitKind names one class of time a command spends waiting rather than
// computing. Only observed waits are recorded; nothing is inferred.
type WaitKind int

const (
	// WaitHTTP covers one request from send through the response headers.
	WaitHTTP WaitKind = iota + 1
	// WaitFileLock covers observed file-lock acquisition. Cache index locks
	// block, while the workspace writer's nonblocking try contributes only
	// the measured attempt.
	WaitFileLock
)

// maxObservedWait bounds one wait sample and the cumulative total so a
// suspended machine or a clock jump cannot distort an event.
const maxObservedWait = 7 * 24 * time.Hour

// WaitStart marks the start of a wait when this logger captures telemetry.
// It returns the zero time otherwise, and WaitDone ignores a zero start.
// Calling it costs a time sample only; no timers or allocations are made.
func (l *Logger) WaitStart() time.Time {
	if l == nil || !l.captureWait.Load() {
		return time.Time{}
	}
	return time.Now()
}

// WaitDone records the elapsed time since start for kind. Multiple samples of
// the same kind accumulate; different kinds stay separate. Samples may span
// overlapping work, so their totals are observed waits, not exclusive stages.
func (l *Logger) WaitDone(kind WaitKind, start time.Time) {
	if l == nil || start.IsZero() || !l.captureWait.Load() {
		return
	}
	elapsed := time.Since(start)
	if elapsed < 0 {
		return
	}
	elapsed = min(elapsed, maxObservedWait)
	l.waitMu.Lock()
	defer l.waitMu.Unlock()
	switch kind {
	case WaitHTTP:
		l.waitHTTP = min(l.waitHTTP+elapsed, maxObservedWait)
		l.waitHTTPSampled = true
	case WaitFileLock:
		l.waitLock = min(l.waitLock+elapsed, maxObservedWait)
		l.waitLockSampled = true
	}
}

// WaitTimings returns the observed HTTP and workspace lock waits. A nil
// result means the kind was never sampled, which is distinct from a measured
// zero wait.
func (l *Logger) WaitTimings() (http, lock *time.Duration) {
	if l == nil {
		return nil, nil
	}
	l.waitMu.Lock()
	defer l.waitMu.Unlock()
	if l.waitHTTPSampled {
		value := l.waitHTTP
		http = &value
	}
	if l.waitLockSampled {
		value := l.waitLock
		lock = &value
	}
	return http, lock
}
