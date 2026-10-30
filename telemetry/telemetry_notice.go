package telemetry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// noticeMarkerName is the file whose presence means the one-time telemetry
// notice was already presented on this machine. The misspelling is deliberate:
// the file name is a compatibility surface, so correcting it would present the
// notice again on every machine that already dismissed it.
const noticeMarkerName = ".alpha_telemety_notice_shown"

// ClaimNotice reports whether this process must present the one-time telemetry
// notice, and records the claim so no later process presents it again.
//
// The claim is the exclusive creation of the marker file beside the local
// state, so invocations that start together present the notice at most once.
// The marker exists before the caller renders anything: a claim is permanent
// even when the caller cannot print.
func (c *Client) ClaimNotice() (bool, error) {
	if c.disabled {
		return false, nil
	}
	if err := os.MkdirAll(c.directory, 0o700); err != nil {
		return false, fmt.Errorf("telemetry: create state directory: %w", err)
	}
	file, err := os.OpenFile(
		filepath.Join(c.directory, noticeMarkerName),
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		0o600,
	)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("telemetry: claim notice: %w", err)
	}
	_ = file.Close()
	syncDirectory(c.directory)
	return true, nil
}
