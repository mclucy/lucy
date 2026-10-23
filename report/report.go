// Package report presents warnings and errors on stderr independently of
// diagnostic logging.
package report

// Info presents a non-fatal observation on stderr.
func Info(message string) {
	console.Info(message)
}

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
