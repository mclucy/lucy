package remote

import (
	"errors"
	"fmt"
)

var (
	ErrorSourceNotSupported  = errors.New("source not supported")
	ErrorCannotInferPlatform = errors.New("cannot infer platform")
	ErrorCannotInferSource   = errors.New("cannot infer source")
	ErrorCannotInferVersion  = errors.New("cannot infer version")
	ErrorNoPackage           = errors.New("no such package")
	ErrorNoResults           = errors.New("no results found")
	ErrorNoVersion           = errors.New("no version found")
	ErrorUnsupportedPlatform = errors.New("unsupported platform")
)

// FormatRemoteError is ONLY for errors related with remote operations
func FormatRemoteError(err error, args ...interface{}) error {
	switch len(args) {
	case 0:
		return err
	case 1:
		return fmt.Errorf("%w: %v", err, args[0])
	case 2:
		return fmt.Errorf("%w for %v: %v", err, args[0], args[1])
	default:
		return fmt.Errorf("%w: %v", err, args)
	}
}
