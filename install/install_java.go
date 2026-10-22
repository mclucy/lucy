package install

import (
	"context"

	"github.com/mclucy/lucy/java"
	"github.com/mclucy/lucy/lockfile"
)

// selectJava selects the Java runtime of the locked server. When the
// selection misses the requirement and the caller can provision a runtime,
// the caller runs first and the runtime is selected again.
func selectJava(ctx context.Context, root string, resolved *lockfile.Document, opts Options) (*java.Check, error) {
	requirement := java.RequirementOf(resolved)
	check, err := java.Select(ctx, root, requirement)
	if err != nil {
		return nil, err
	}
	if opts.ProvisionJava == nil || check.Satisfied() {
		return check, nil
	}
	if err := opts.ProvisionJava(ctx, requirement, check); err != nil {
		return nil, err
	}
	return java.Select(ctx, root, requirement)
}
