package tui

import "testing"

func testLogoVariant(t *testing.T, variant LogoVariant, name string) {
	t.Helper()
	f := &FieldLogo{}

	lines := f.Lines(variant)
	width := f.Width(variant)
	height := f.Height(variant)

	if width <= 0 {
		t.Fatalf("%s: Width() = %d, want > 0", name, width)
	}
	if height <= 0 {
		t.Fatalf("%s: Height() = %d, want > 0", name, height)
	}
	if height != len(lines) {
		t.Fatalf("%s: Height() = %d, but len(Lines()) = %d", name, height, len(lines))
	}

	for i, line := range lines {
		runeLen := len([]rune(line))
		if runeLen != width {
			t.Errorf("%s: line %d has %d runes, want %d (uniform width)", name, i, runeLen, width)
		}
	}

	t.Logf("%s: Width=%d, Height=%d", name, width, height)
}

func TestStatusLogoLarge(t *testing.T) {
	testLogoVariant(t, LogoLarge, "LogoLarge")

	f := &FieldLogo{}
	if f.KeyLength() != 0 {
		t.Errorf("KeyLength() = %d, want 0", f.KeyLength())
	}

	rendered := f.Render()
	if len(rendered) == 0 {
		t.Fatal("Render() returned empty string")
	}
}

func TestStatusLogoSmall(t *testing.T) {
	testLogoVariant(t, LogoSmall, "LogoSmall")
}

func TestNormalizeLinesEmpty(t *testing.T) {
	result := normalizeLines("")
	if result != nil {
		t.Errorf("normalizeLines(\"\") = %v, want nil", result)
	}

	result = normalizeLines("\n\n\n")
	if result != nil {
		t.Errorf("normalizeLines(\"\\n\\n\\n\") = %v, want nil", result)
	}
}
