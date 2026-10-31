//go:build !windows && !unix && !linux && !darwin

package workspace

// checkSessionLock has no implementation on platforms without a supported
// locking primitive; activity is simply unobservable there.
func checkSessionLock(string) bool { return false }
