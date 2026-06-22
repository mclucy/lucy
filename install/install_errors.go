package install

import "fmt"

type ErrorCategory int

const (
	CategoryConflict ErrorCategory = iota
	CategoryResolution
	CategoryDownload
	CategoryVerify
	CategoryApply
)

func (c ErrorCategory) String() string {
	switch c {
	case CategoryConflict:
		return "conflict"
	case CategoryResolution:
		return "resolution"
	case CategoryDownload:
		return "download"
	case CategoryVerify:
		return "verify"
	case CategoryApply:
		return "apply"
	default:
		return "unknown"
	}
}

type InstallError struct {
	Category ErrorCategory
	Cause    error
	Context  map[string]any
}

func (e *InstallError) Error() string {
	if e == nil {
		return "install: unknown error"
	}
	if e.Cause == nil {
		return fmt.Sprintf("install: %s failed", e.Category)
	}
	return fmt.Sprintf("install: %s failed: %v", e.Category, e.Cause)
}

func (e *InstallError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func installError(category ErrorCategory, cause error, context map[string]any) error {
	if cause == nil {
		return nil
	}
	return &InstallError{Category: category, Cause: cause, Context: context}
}
