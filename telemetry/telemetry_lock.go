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
// duration waits that long, and a negative duration waits until ctx ends.
func (c *Client) enterOperation(ctx context.Context, wait time.Duration) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	lock, err := c.lockState(ctx, wait)
	if err != nil {
		<-c.sem
		return nil, err
	}
	return func() {
		lock.release()
		<-c.sem
	}, nil
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
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
