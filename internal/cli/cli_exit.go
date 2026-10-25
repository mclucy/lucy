package cli

import "strconv"

// ExitError carries an explicit process exit status through the command
// boundary. Silent suppresses error rendering for statuses that are not
// failures, such as a search with no matches.
type ExitError struct {
	Code   int
	Err    error
	Silent bool
}

// ExitWith reports a failure with a specific process exit status.
func ExitWith(code int, err error) error {
	return &ExitError{
		Code: code,
		Err:  err,
	}
}

// SilentExit reports a status without an error message.
func SilentExit(code int) error {
	return &ExitError{
		Code:   code,
		Silent: true,
	}
}

func (e *ExitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return "exit status " + strconv.Itoa(e.Code)
}

func (e *ExitError) Unwrap() error {
	return e.Err
}

func (e *ExitError) ExitStatus() int {
	return e.Code
}

func (e *ExitError) SilentExit() bool {
	return e.Silent
}
