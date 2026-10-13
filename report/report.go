// Package report presents user-facing messages on stderr independently of diagnostic logging.
package report

import (
	"charm.land/log/v2"
)

func Info(content any) {
	console.Info(content)
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

// Fatal reports a fatal error without terminating the process.
func Fatal(err error) {
	if err != nil {
		console.Log(log.FatalLevel, err.Error())
	}
}
