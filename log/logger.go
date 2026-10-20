// Package log records operational diagnostics independently of user reports.
package log

import "charm.land/log/v2"

func Info(content any, fields ...any) {
	write(log.InfoLevel, content, fields...)
}

func Warn(err error) {
	if err != nil {
		write(log.WarnLevel, err)
	}
}

func Error(err error) {
	if err != nil {
		write(log.ErrorLevel, err)
	}
}

func Debug(content any, fields ...any) {
	write(log.DebugLevel, content, fields...)
}

func write(level log.Level, content any, fields ...any) {
	mu.RLock()
	defer mu.RUnlock()
	diagnostic.Log(level, content, fields...)
}
