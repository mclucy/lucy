package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

// newJSONFlagCommand builds the flag surface a real leaf command sees: the
// output switches inherited from the root plus a value-taking flag that can
// appear before a JSON switch.
func newJSONFlagCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "leaf"}
	cmd.PersistentFlags().Bool(FlagJSON, false, "Print JSON indented with 2 spaces")
	cmd.PersistentFlags().Bool(FlagJSONCompact, false, "Print JSON without indentation")
	AddEcosystemFlag(cmd)
	return cmd
}

func TestJSONModeFromArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want JSONRendering
	}{
		{name: "plain", args: nil, want: JSONNone},
		{name: "pretty", args: []string{"--json"}, want: JSONPretty},
		{name: "compact only", args: []string{"--json-compact"}, want: JSONCompact},
		{
			name: "compact wins",
			args: []string{"--json", "--json-compact"},
			want: JSONCompact,
		},
		{name: "pretty disabled", args: []string{"--json=false"}, want: JSONNone},
		{
			name: "compact disabled",
			args: []string{"--json", "--json-compact=false"},
			want: JSONPretty,
		},
		{
			// A flag that takes a value must swallow the following switch
			// instead of letting it select the output rendering.
			name: "value consumed",
			args: []string{"--ecosystem", "--json"},
			want: JSONNone,
		},
		{
			name: "joined value",
			args: []string{"--ecosystem=--json"},
			want: JSONNone,
		},
		{
			name: "after terminator",
			args: []string{"--", "--json"},
			want: JSONNone,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := newJSONFlagCommand()
			if got := JSONModeFromArgs(cmd, test.args); got != test.want {
				t.Fatalf("JSONModeFromArgs(%q) = %v, want %v", test.args, got, test.want)
			}
		})
	}
}

func TestJSONFlagsPresent(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "empty", args: nil, want: false},
		{name: "pretty", args: []string{"--json"}, want: true},
		{name: "pretty disabled", args: []string{"--json=false"}, want: true},
		{name: "compact", args: []string{"--json-compact"}, want: true},
		{
			name: "compact disabled",
			args: []string{"--json-compact=false"},
			want: true,
		},
		{
			name: "before terminator",
			args: []string{"--json", "--"},
			want: true,
		},
		{name: "after terminator", args: []string{"--", "--json"}, want: false},
		{name: "longer name", args: []string{"--jsonify"}, want: false},
		{name: "single dash", args: []string{"-json"}, want: false},
		{
			name: "longer name with value",
			args: []string{"--json-compactness=yes"},
			want: false,
		},
		{
			// JSONFlagsPresent scans raw arguments and cannot know that
			// --ecosystem takes a value, so a swallowed --json still counts.
			// JSONModeFromArgs is the value-aware parser.
			name: "consumed by a value-taking flag",
			args: []string{"--ecosystem", "--json"},
			want: true,
		},
		{
			name: "value of another flag",
			args: []string{"--ecosystem", "-json"},
			want: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := JSONFlagsPresent(test.args); got != test.want {
				t.Fatalf("JSONFlagsPresent(%q) = %v, want %v", test.args, got, test.want)
			}
		})
	}
}
