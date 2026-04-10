package install

import (
	"testing"

	"github.com/mclucy/lucy/upstream"
)

func TestSelectFromCandidates_NoPanicWithCandidates(t *testing.T) {
	candidates := []upstream.FetchResult{
		{},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("selectFromCandidates panicked: %v", r)
		}
	}()

	_, _ = selectFromCandidates(candidates)
}
