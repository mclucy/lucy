package bootstrap

import "time"

// Progress receives preparation facts from a bootstrap adapter. A zero-value
// callback reports nothing; the caller decides how to present them.
type Progress struct {
	PhaseStarted  func(name string)
	PhaseProgress func(name string, count, total int)
	PhaseDone     func(name string, elapsed time.Duration, count, total int)
}

func (p Progress) started(name string) {
	if p.PhaseStarted != nil {
		p.PhaseStarted(name)
	}
}

func (p Progress) progressed(name string, count, total int) {
	if p.PhaseProgress != nil {
		p.PhaseProgress(name, count, total)
	}
}

func (p Progress) done(name string, elapsed time.Duration, count, total int) {
	if p.PhaseDone != nil {
		p.PhaseDone(name, elapsed, count, total)
	}
}
