package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/server"
)

// CallDaemonWithAutoStart sends req to the daemon, starting the daemon
// service once and retrying when the socket is not answering.
func CallDaemonWithAutoStart(ctx context.Context, req server.Request, out any) error {
	err := server.CallDaemon(ctx, req, out)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil || !errors.Is(err, server.ErrIPCUnavailable) {
		return err
	}
	log.ShowInfo("Lucy daemon is not responding; attempting to start it")
	if startErr := server.NewServiceManager().(server.DaemonStarter).StartDaemon(); startErr != nil {
		return fmt.Errorf("start Lucy daemon: %w (original request failed: %v)", startErr, err)
	}
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	return server.CallDaemon(ctx, req, out)
}
