package install

import (
	"context"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/javaruntime"
)

// selectJava selects the Java runtime of the locked server. When the
// selection misses the requirement and the caller can provision a runtime,
// the caller runs first and the runtime is selected again.
func (s *Service) selectJava(ctx context.Context, root string, resolved *document.Lock, opts Options) (*javaruntime.Check, error) {
	requirement := javaruntime.RequirementOf(resolved, s.logger)
	check, err := javaruntime.Select(ctx, root, requirement, s.logger)
	if err != nil {
		return nil, err
	}
	if opts.ProvisionJava == nil || check.Satisfied() {
		return check, nil
	}
	home, err := opts.ProvisionJava(ctx, requirement, check)
	if err != nil {
		return nil, err
	}
	return javaruntime.Select(ctx, root, requirement, s.logger, home)
}
