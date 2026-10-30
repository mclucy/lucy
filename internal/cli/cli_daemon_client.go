package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mclucy/lucy/log"
)

// CallDaemonWithAutoStart sends req to the daemon, starting the daemon
// service once and retrying when the socket is not answering.
func CallDaemonWithAutoStart(ctx context.Context, req Request, out any, logger *log.Logger) error {
	err := CallDaemon(ctx, req, out)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil || !errors.Is(err, ErrIPCUnavailable) {
		return err
	}
	logger.Info("Lucy daemon is not responding; attempting to start it")
	if startErr := NewServiceManager().(DaemonStarter).StartDaemon(); startErr != nil {
		return fmt.Errorf("start Lucy daemon: %w (original request failed: %v)", startErr, err)
	}
	readiness, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-readiness.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("daemon did not become ready within 10 seconds: %w", err)
		case <-ticker.C:
		}
		err = CallDaemon(ctx, req, out)
		if err == nil || !errors.Is(err, ErrIPCUnavailable) {
			return err
		}
	}
}
