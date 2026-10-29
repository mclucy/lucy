package compile

import (
	"context"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/internal/cli"
)

// promptModProject asks which of several mod projects to compile. `huh` renders
// on stderr, so stdout keeps carrying only the artifact path. Aborting the
// selection cancels the compilation; there is no default to fall back to.
func promptModProject(
	ctx context.Context,
	candidates []gradleProject,
	gate cli.PromptGate,
) (gradleProject, error) {
	// gradleProject is not comparable (it holds slices), so the select carries
	// the candidate index instead of the struct.
	options := make([]huh.Option[int], 0, len(candidates))
	for i, project := range candidates {
		options = append(options, huh.NewOption(describeModProject(project), i))
	}
	var picked int
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[int]().
			Title("Select a mod project").
			Description("Multiple Forge, NeoForge, or Fabric mod projects were found.").
			Options(options...).
			Filtering(true).
			Value(&picked),
	)).
		WithInput(gate.Input()).
		WithOutput(gate.Output())
	if err := cli.NormalizePromptError(ctx, form.RunWithContext(ctx)); err != nil {
		return gradleProject{}, err
	}
	return candidates[picked], nil
}

// describeModProject labels a candidate for the select prompt. Unlike the
// non-interactive error, it names the root project in words.
func describeModProject(project gradleProject) string {
	return nameProject(project.Path) + " [" + joinEcosystems(project.Ecosystems) + "]"
}
