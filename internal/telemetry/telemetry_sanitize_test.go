package telemetry

import "testing"

func TestRedactReportTextCredentials(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "quoted flag value",
			input: `--password="correct horse battery staple"`,
			want:  "--password=[redacted]",
		},
		{
			name:  "quoted flag segment with suffix",
			input: `--password "abc"def`,
			want:  "--password [redacted]",
		},
		{
			name:  "quoted assignment segment with suffix",
			input: `password: "abc"def`,
			want:  "password=[redacted]",
		},
		{
			name:  "credential before another flag",
			input: `--token "abc"def --verbose`,
			want:  "--token [redacted] --verbose",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := redactReportText(test.input); got != test.want {
				t.Fatalf("redactReportText(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}
