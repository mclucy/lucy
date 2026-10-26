//go:build windows

package telemetry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func checkPrivateDirectory(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect telemetry directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("telemetry path %q is not a directory", path)
	}
	return nil
}

func checkPrivateFile(path string) error {
	_, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect telemetry file: %w", err)
	}
	return nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create telemetry directory: %w", err)
	}
	if err := restrictToCurrentUser(path); err != nil {
		return fmt.Errorf("restrict telemetry directory: %w", err)
	}
	return nil
}

func writePrivateFile(path string, content []byte) error {
	directory := filepath.Dir(path)
	if err := ensurePrivateDirectory(directory); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, content, 0o600); err != nil {
		return err
	}
	if err := restrictToCurrentUser(temporary); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

// restrictToCurrentUser replaces the DACL with one entry for the current user
// and stops inheritance, so the private location does not depend on profile
// directory defaults.
func restrictToCurrentUser(path string) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	entries := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	)
}
