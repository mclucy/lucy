package telemetry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

var errLockBusy = errors.New("telemetry: state is busy")

type stateLock struct {
	file *os.File
}

func (lock *stateLock) release() {
	unlockFile(lock.file)
	_ = lock.file.Close()
}

// enterOperation serializes one state mutation inside the process and, when
// supported, across processes. wait semantics: zero tries once, a positive
// duration is one budget shared by both layers, and a negative duration waits
// until ctx ends.
func (c *Client) enterOperation(ctx context.Context, wait time.Duration) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var deadline time.Time
	if wait > 0 {
		deadline = time.Now().Add(wait)
	}
	if err := c.acquireProcessSlot(ctx, wait, deadline); err != nil {
		return nil, err
	}
	lock, err := c.lockState(ctx, remainingWait(wait, deadline))
	if err != nil {
		<-c.sem
		return nil, err
	}
	return func() {
		lock.release()
		<-c.sem
	}, nil
}

// acquireProcessSlot claims the in-process slot under the same wait budget as
// the file lock, so a caller that asked not to wait never blocks on a
// concurrent operation.
func (c *Client) acquireProcessSlot(ctx context.Context, wait time.Duration, deadline time.Time) error {
	if wait == 0 {
		select {
		case c.sem <- struct{}{}:
			return nil
		default:
			return errLockBusy
		}
	}
	var expired <-chan time.Time
	if wait > 0 {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		expired = timer.C
	}
	select {
	case c.sem <- struct{}{}:
		return nil
	case <-expired:
		return errLockBusy
	case <-ctx.Done():
		return ctx.Err()
	}
}

// remainingWait reports how much of the wait budget is left for the next lock
// layer. Zero means the budget is spent, so that layer tries once.
func remainingWait(wait time.Duration, deadline time.Time) time.Duration {
	if wait <= 0 {
		return wait
	}
	return max(time.Until(deadline), 0)
}

func (c *Client) lockState(ctx context.Context, wait time.Duration) (*stateLock, error) {
	if err := c.ensureDirectories(); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(c.lockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("telemetry: open lock: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("telemetry: secure lock: %w", err)
	}
	var deadline time.Time
	if wait > 0 {
		deadline = time.Now().Add(wait)
	}
	for {
		locked, lockErr := tryFileLock(file)
		if lockErr != nil {
			_ = file.Close()
			return nil, fmt.Errorf("telemetry: lock state: %w", lockErr)
		}
		if locked {
			return &stateLock{file: file}, nil
		}
		if wait == 0 {
			_ = file.Close()
			return nil, errLockBusy
		}
		if wait > 0 && !time.Now().Before(deadline) {
			_ = file.Close()
			return nil, errLockBusy
		}
		poll := 20 * time.Millisecond
		if wait > 0 {
			poll = min(poll, max(time.Until(deadline), 0))
		}
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
