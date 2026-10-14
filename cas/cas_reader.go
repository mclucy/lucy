package cas

import (
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
)

type verifiedReader struct {
	file     *os.File
	expected Digest
	hash     hash.Hash
	finished bool
	err      error
}

func (r *verifiedReader) Read(data []byte) (int, error) {
	if r.finished {
		return 0, r.err
	}
	count, err := r.file.Read(data)
	if count > 0 {
		if _, hashErr := r.hash.Write(data[:count]); hashErr != nil {
			return count, errors.Join(ErrRead, hashErr)
		}
	}
	if errors.Is(err, io.EOF) {
		r.finished = true
		var actual Digest
		r.hash.Sum(actual[:0])
		if actual != r.expected {
			r.err = fmt.Errorf("%w: expected %s, got %s", ErrCorrupt, r.expected.String(), actual.String())
		} else {
			r.err = io.EOF
		}
		return count, r.err
	}
	if err != nil {
		return count, errors.Join(ErrRead, err)
	}
	return count, nil
}

func (r *verifiedReader) Close() error {
	return r.file.Close()
}
