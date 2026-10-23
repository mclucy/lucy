package discovery

import (
	"errors"
	"fmt"
)

var errNotRegularFile = errors.New("discovery: artifact is not a regular file")

// SelectionError is the typed failure of a selection request. It carries the
// domain state and the available installations so the presentation boundary
// can derive its own guidance; no CLI command or severity strings live here.
type SelectionError struct {
	State         SelectionState
	Path          string
	Installations []Installation
}

func (e *SelectionError) Error() string {
	if e == nil {
		return "server selection failed"
	}
	switch e.State {
	case SelectionNone:
		return "no server installation was found"
	case SelectionAmbiguous:
		return "multiple server installations satisfy the selection"
	case SelectionMissingDeclared:
		if e.Path != "" {
			return fmt.Sprintf(
				"declared server entry point %q is not installed",
				e.Path,
			)
		}
		return "the declared server runtime is not installed"
	case SelectionInvalid:
		if e.Path != "" {
			return fmt.Sprintf(
				"requested entry point %q is not a recognized server entry point",
				e.Path,
			)
		}
		return "no launchable server installation was found"
	default:
		return "server selection failed"
	}
}
