package install

import (
	"os"
	"testing"

	"github.com/mclucy/lucy/upstream"
)

func TestSelectFromCandidates_NoPanicWithCandidates(t *testing.T) {
	candidates := []upstream.FetchResult{
		{},
	}

	// Replace os.Stdin with a pipe that returns EOF immediately so the test
	// does not depend on the CI runner's stdin wiring (which may block or
	// behave differently across environments).
	origStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	w.Close() // closing the write end makes reads return EOF
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin; r.Close() })

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("selectFromCandidates panicked: %v", r)
		}
	}()

	_, _ = selectFromCandidates(candidates)
}
