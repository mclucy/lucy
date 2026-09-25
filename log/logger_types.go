package log

import (
	"time"

	"charm.land/log/v2"
)

// entry is one history-buffer record.
type entry struct {
	Time    time.Time
	Level   log.Level
	Content any
}
