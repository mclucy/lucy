// Package report presents warnings and errors on stderr independently of
// diagnostic logging.
package report

func Warn(err error) {
	if err != nil {
		console.Warn(err.Error())
	}
}

func Error(err error) {
	if err != nil {
		console.Error(err.Error())
	}
}
