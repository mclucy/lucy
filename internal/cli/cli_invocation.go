package cli

import (
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/terminal/style"
)

// Invocation is the state of one `lucy` run that every command shares:
// where the run operates, which diagnostics it may write, and how it may
// present results. It carries no services and no ambient lookups; commands
// receive the services they use as explicit constructor parameters.
//
// Directory is resolved before actions run. Theme, Stdout, Stderr, and
// Interactive depend on parsed persistent flags, so the root wiring fills
// them before the action executes.
type Invocation struct {
	// Directory is the absolute directory the command operates on. Server
	// selection resolves it; handlers never read or change the process
	// working directory.
	Directory string
	// Logger receives diagnostics for this run. A nil Logger deliberately
	// records nothing.
	Logger *log.Logger
	// Theme styles human output.
	Theme style.Theme
	// Stdout describes the standard output terminal of this run.
	Stdout style.Terminal
	// Stderr describes the standard error terminal of this run.
	Stderr style.Terminal
	// Interactive reports whether prompts and animated output are allowed.
	Interactive bool
}

// NewInvocation prepares the per-run state for directory. Presentation
// fields stay at their no-color, pipe defaults until the root wiring
// configures them.
func NewInvocation(directory string, logger *log.Logger) *Invocation {
	return &Invocation{
		Directory:   directory,
		Logger:      logger,
		Theme:       style.New(false),
		Stdout:      style.Terminal{Width: 80, Height: 24},
		Stderr:      style.Terminal{Width: 80, Height: 24},
		Interactive: false,
	}
}
