package imgprotocol

import "testing"

func TestDetectReturnsNoneWhenStdoutIsNotTerminal(t *testing.T) {
	got := Detect()
	if got != None {
		t.Fatalf("Detect() = %s, want %s", got, None)
	}
}
