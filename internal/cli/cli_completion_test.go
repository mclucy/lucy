package cli

import (
	"slices"
	"testing"
)

// completionValues extracts the values of candidates for comparison.
func completionValues(candidates []CompletionCandidate) []string {
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, candidate.Value)
	}
	return values
}

func TestCompletionFiltering(t *testing.T) {
	candidates := []CompletionCandidate{
		{Value: "fabric", Description: "Fabric mods"},
		{Value: "forge", Description: "Forge mods"},
		{Value: "neoforge", Description: "NeoForge mods"},
		{Value: "paper", Description: "Paper server"},
	}

	t.Run("filter by prefix", func(t *testing.T) {
		wantAll := []string{"fabric", "forge", "neoforge", "paper"}
		tests := []struct {
			name   string
			prefix string
			want   []string
		}{
			{name: "empty", prefix: "", want: wantAll},
			{name: "exact", prefix: "forge", want: []string{"forge"}},
			{name: "case insensitive", prefix: "FA", want: []string{"fabric"}},
			{name: "shared prefix", prefix: "neo", want: []string{"neoforge"}},
			{name: "no match", prefix: "zzz"},
		}

		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				got := completionValues(FilterByPrefix(candidates, test.prefix))
				if test.want == nil {
					if len(got) != 0 {
						t.Fatalf("FilterByPrefix(%q) = %q, want none", test.prefix, got)
					}
					return
				}
				if !slices.Equal(got, test.want) {
					t.Fatalf("FilterByPrefix(%q) = %q, want %q", test.prefix, got, test.want)
				}
			})
		}
	})
}
