package tui

import "testing"

func TestNegotiateStatusLayout(t *testing.T) {
	const (
		largeLogo = 40
		smallLogo = 20
	)

	tests := []struct {
		name      string
		termWidth int
		isTTY     bool
		wantMode  StatusLayoutMode
		wantLogo  int
		wantInfo  int
		wantGap   int
	}{
		{
			name:      "non-TTY returns InfoOnly",
			termWidth: 200,
			isTTY:     false,
			wantMode:  LayoutInfoOnly,
			wantLogo:  0,
			wantInfo:  200,
			wantGap:   0,
		},
		{
			name:      "wide terminal uses large side-by-side",
			termWidth: 200,
			isTTY:     true,
			wantMode:  LayoutLargeLogoSideBySide,
			wantLogo:  largeLogo,
			wantInfo:  200 - largeLogo - 3,
			wantGap:   3,
		},
		{
			name:      "medium terminal uses small side-by-side",
			termWidth: 80,
			isTTY:     true,
			wantMode:  LayoutSmallLogoSideBySide,
			wantLogo:  smallLogo,
			wantInfo:  80 - smallLogo - 3,
			wantGap:   3,
		},
		{
			name:      "exact large threshold 83",
			termWidth: 83,
			isTTY:     true,
			wantMode:  LayoutLargeLogoSideBySide,
			wantLogo:  largeLogo,
			wantInfo:  83 - largeLogo - 3,
			wantGap:   3,
		},
		{
			name:      "exact small threshold 63",
			termWidth: 63,
			isTTY:     true,
			wantMode:  LayoutSmallLogoSideBySide,
			wantLogo:  smallLogo,
			wantInfo:  63 - smallLogo - 3,
			wantGap:   3,
		},
		{
			name:      "below small threshold 62 falls to vertical",
			termWidth: 62,
			isTTY:     true,
			wantMode:  LayoutVertical,
			wantLogo:  largeLogo,
			wantInfo:  62,
			wantGap:   0,
		},
		{
			name:      "exact minInfoWidth 40 vertical",
			termWidth: 40,
			isTTY:     true,
			wantMode:  LayoutVertical,
			wantLogo:  largeLogo,
			wantInfo:  40,
			wantGap:   0,
		},
		{
			name:      "below minInfoWidth 39 clipped",
			termWidth: 39,
			isTTY:     true,
			wantMode:  LayoutClipped,
			wantLogo:  0,
			wantInfo:  39,
			wantGap:   0,
		},
		{
			name:      "width 1 clipped no panic",
			termWidth: 1,
			isTTY:     true,
			wantMode:  LayoutClipped,
			wantLogo:  0,
			wantInfo:  1,
			wantGap:   0,
		},
		{
			name:      "width 0 clipped",
			termWidth: 0,
			isTTY:     true,
			wantMode:  LayoutClipped,
			wantLogo:  0,
			wantInfo:  0,
			wantGap:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NegotiateStatusLayout(tt.termWidth, largeLogo, smallLogo, tt.isTTY)

			if got.Mode != tt.wantMode {
				t.Errorf("Mode = %d, want %d", got.Mode, tt.wantMode)
			}
			if got.LogoWidth != tt.wantLogo {
				t.Errorf("LogoWidth = %d, want %d", got.LogoWidth, tt.wantLogo)
			}
			if got.InfoWidth != tt.wantInfo {
				t.Errorf("InfoWidth = %d, want %d", got.InfoWidth, tt.wantInfo)
			}
			if got.GapWidth != tt.wantGap {
				t.Errorf("GapWidth = %d, want %d", got.GapWidth, tt.wantGap)
			}
			if got.InfoWidth < 0 {
				t.Errorf("InfoWidth is negative: %d", got.InfoWidth)
			}
		})
	}
}
