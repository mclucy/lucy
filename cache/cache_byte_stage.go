package cache

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/log"
)

// byteStage can finish a buffered response without requiring temporary disk space.
type byteStage struct {
	writer *cas.Writer
	hasher hash.Hash
}

func newByteStage() *byteStage {
	writer, err := Network().newWriter()
	if err == nil {
		return &byteStage{writer: writer}
	}
	if !errors.Is(err, ErrDisabled) {
		log.Warn(fmt.Errorf("cache staging unavailable: %w", err))
	}
	return &byteStage{hasher: sha256.New()}
}

func (s *byteStage) detach() error {
	hasher, err := s.writer.DetachHash()
	if err != nil {
		return err
	}
	if err := s.writer.Abort(); err != nil {
		log.Warn(fmt.Errorf("remove failed cache stage: %w", err))
	}
	s.hasher = hasher
	s.writer = nil
	return nil
}

func (s *byteStage) Write(data []byte) (int, error) {
	if s.writer == nil {
		return s.hasher.Write(data)
	}
	count, err := s.writer.Write(data)
	if err == nil {
		return count, nil
	}
	log.Warn(fmt.Errorf("cache write failed, retaining buffered response: %w", err))
	if err := s.detach(); err != nil {
		return count, err
	}
	rest, err := s.hasher.Write(data[count:])
	return count + rest, err
}

func (s *byteStage) seal() (cas.Object, error) {
	if s.writer != nil {
		object, err := s.writer.Seal(nil)
		if err == nil {
			return object, nil
		}
		log.Warn(fmt.Errorf("cache sync failed, retaining buffered response: %w", err))
		if err := s.detach(); err != nil {
			return cas.Object{}, err
		}
	}
	var digest cas.Digest
	s.hasher.Sum(digest[:0])
	return cas.Object{Digest: digest}, nil
}

func (s *byteStage) Close() error {
	if s.writer != nil {
		return s.writer.Abort()
	}
	return nil
}
