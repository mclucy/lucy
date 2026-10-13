package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/resolve"
)

// LucyStateDirExists reports whether workDir holds a project manifest.
func LucyStateDirExists(workDir string) (bool, error) {
	info, err := os.Stat(filepath.Join(workDir, manifest.Filename))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", manifest.Filename, err)
	}
	return !info.IsDir(), nil
}

func FormatConstraintConflict(err *resolve.ConstraintConflictError) error {
	if err == nil {
		return fmt.Errorf("dependency constraints conflict")
	}

	return fmt.Errorf(
		"dependency constraints conflict for %s: %s requires %s, %s requires %s",
		err.PackageId.StringBase(),
		err.Left.Requester,
		resolve.FormatVersionConstraint(err.Left.Constraint),
		err.Right.Requester,
		resolve.FormatVersionConstraint(err.Right.Constraint),
	)
}
