package state

import (
	"fmt"
	"strings"
)

type ErrorKind string

const (
	ErrMalformed ErrorKind = "malformed"
	ErrIOFailure ErrorKind = "io_failure"
)

type StateError struct {
	File  StateFile
	Kind  ErrorKind
	Field string
	Msg   string
}

func NewStateError(file StateFile, kind ErrorKind, field, msg string) StateError {
	return StateError{File: file, Kind: kind, Field: field, Msg: msg}
}

func (e StateError) Error() string {
	parts := make([]string, 0, 4)
	if e.File != "" {
		parts = append(parts, string(e.File))
	}
	if e.Field != "" {
		parts = append(parts, e.Field)
	}
	if e.Kind != "" {
		parts = append(parts, string(e.Kind))
	}
	if e.Msg != "" {
		parts = append(parts, e.Msg)
	}
	if len(parts) == 0 {
		return "state error"
	}
	return strings.Join(parts, ": ")
}

func malformedStateError(file StateFile, field string, err error) error {
	if err == nil {
		return nil
	}
	return NewStateError(file, ErrMalformed, field, err.Error())
}

func ioStateError(file StateFile, field, msg string, err error) error {
	if err == nil {
		return NewStateError(file, ErrIOFailure, field, msg)
	}
	return NewStateError(file, ErrIOFailure, field, fmt.Sprintf("%s: %v", msg, err))
}
