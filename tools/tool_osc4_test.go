package tools

import (
	"testing"

	"github.com/muesli/termenv"
)

func TestOSC4GuardUsesTermenvSafeProfileDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		profile termenv.Profile
		want    bool
	}{
		{
			name:    "skip ascii profile",
			profile: termenv.Ascii,
			want:    false,
		},
		{
			name:    "skip ansi profile",
			profile: termenv.ANSI,
			want:    false,
		},
		{
			name:    "skip ansi256 profile",
			profile: termenv.ANSI256,
			want:    false,
		},
		{
			name:    "allow truecolor profile",
			profile: termenv.TrueColor,
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := shouldQueryOSC4(tt.profile)
			if got != tt.want {
				t.Fatalf("shouldQueryOSC4(%v) = %v, want %v", tt.profile, got, tt.want)
			}
		})
	}
}
