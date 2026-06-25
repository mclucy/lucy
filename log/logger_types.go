package log

import (
	"time"

	"charm.land/log/v2"
)

// entry represents a single log item for the history buffer.
// Uses charm/log's Level type directly.
type entry struct {
	Time    time.Time
	Level   log.Level
	Content any
}
