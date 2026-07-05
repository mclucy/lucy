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
	var responseErr server.ResponseError
	if errors.As(err, &responseErr) {
		return err
	}
	log.ShowInfo("Lucy daemon is not responding; attempting to start it")
	if startErr := server.NewServiceManager().StartDaemon(); startErr != nil {
		return fmt.Errorf("start Lucy daemon: %w (original request failed: %v)", startErr, err)
	}
	time.Sleep(500 * time.Millisecond)
	return server.CallDaemon(ctx, req, out)
}
