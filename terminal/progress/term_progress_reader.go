package progress

import "io"

type proxyReader struct {
	io.Reader
	tracker *Tracker
	total   int64
	read    int64
}

func (r *proxyReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.read += int64(n)
	if r.total > 0 {
		r.tracker.setBytesProgress(r.read, r.total)
	}
	return n, err
}
