package resolve

// Trace receives facts about one resolution as they happen. Every field is
// optional: a nil field or a nil Trace reports nothing.
//
// Hooks run on the goroutine that searches, one call at a time. A caller that
// resolves concurrently must synchronize its own sink.
type Trace struct {
	// State reports how many projects are chosen and how many requests remain.
	State func(resolved, pending int)
	// Selected reports the candidate that entered the graph.
	Selected func(project, version string)
	// Rejected reports a candidate that did not enter the graph. The reason is
	// the same text the failure summary groups, without the package's name.
	Rejected func(project, version, reason string)
}

func (t Trace) state(resolved, pending int) {
	if t.State != nil {
		t.State(resolved, pending)
	}
}

func (t Trace) selected(project, version string) {
	if t.Selected != nil {
		t.Selected(project, version)
	}
}

func (t Trace) rejected(project, version, reason string) {
	if t.Rejected != nil {
		t.Rejected(project, version, reason)
	}
}
