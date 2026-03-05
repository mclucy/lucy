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

	// Test CRLF line endings
	result = normalizeLines("line1\r\nline2\r\n")
	if len(result) != 2 {
		t.Errorf("normalizeLines(\"line1\\r\\nline2\\r\\n\") returned %d lines, want 2", len(result))
	}
	if result[0] != "line1" {
		t.Errorf("normalizeLines(\"line1\\r\\nline2\\r\\n\")[0] = %q, want %q", result[0], "line1")
	}
	if result[1] != "line2" {
		t.Errorf("normalizeLines(\"line1\\r\\nline2\\r\\n\")[1] = %q, want %q", result[1], "line2")
	}

	// Test single-line input without trailing newline
	result = normalizeLines("hello")
	if len(result) != 1 {
		t.Errorf("normalizeLines(\"hello\") returned %d lines, want 1", len(result))
	}
	if result[0] != "hello" {
		t.Errorf("normalizeLines(\"hello\")[0] = %q, want %q", result[0], "hello")
	}
}
