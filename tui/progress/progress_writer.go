package progress

type logWriter struct {
	tracker *Tracker
}

func (w *logWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.tracker.appendLog(string(p))
	}
	return len(p), nil
}
