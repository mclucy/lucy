package discovery

import "errors"

var errNotRegularFile = errors.New("discovery: artifact is not a regular file")

// SelectionError is the typed failure of a selection request. It carries the
// domain state and the available installations so the presentation boundary
// can derive its own guidance; no CLI command or severity strings live here.
type SelectionError struct {
	State         SelectionState
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
		return "the declared server installation is not installed"
	case SelectionUnmatched:
		return "no installed server matches the requested filters"
	case SelectionInvalid:
		return "no launchable server installation was found"
	default:
		return "server selection failed"
	}
}
