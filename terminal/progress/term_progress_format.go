package progress

import (
	"fmt"
	"time"

	"github.com/mclucy/lucy/terminal/style"
)

// FormatElapsed renders a phase duration for people: 0.06s, 35.8s, 1m 02s.
func FormatElapsed(elapsed time.Duration) string {
	switch {
	case elapsed < time.Second:
		return fmt.Sprintf("%.2fs", elapsed.Seconds())
	case elapsed < time.Minute:
		return fmt.Sprintf("%.1fs", elapsed.Seconds())
	default:
		minutes := int(elapsed / time.Minute)
		seconds := int((elapsed % time.Minute) / time.Second)
		return fmt.Sprintf("%dm %02ds", minutes, seconds)
	}
}

func phaseLine(text string, elapsed time.Duration, theme style.Theme) string {
	return theme.Success("✓") + " " + text + " " + theme.Muted("in "+FormatElapsed(elapsed))
}
