package cache

import (
	"errors"
	"fmt"
	"os"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/log"
)

// downloadStage retains the stream when optional cache storage cannot accept it.
type downloadStage struct {
	writer    *cas.Writer
	cacheable bool
	temporary *cas.Store
	directory string
}

func newDownloadStage() (*downloadStage, error) {
	writer, err := Network().newWriter()
	stage := &downloadStage{writer: writer, cacheable: err == nil}
	if err == nil {
		return stage, nil
	}
	if !errors.Is(err, ErrDisabled) {
		log.Warn(fmt.Errorf("cache staging unavailable: %w", err))
	}
	if err := stage.fallback(); err != nil {
		return nil, err
	}
	return stage, nil
}

func (s *downloadStage) fallback() error {
	directory, err := os.MkdirTemp("", "lucy-download-")
	if err != nil {
		return err
	}
	store, err := cas.New(directory)
	if err != nil {
		return errors.Join(err, os.RemoveAll(directory))
	}
	var writer *cas.Writer
	if s.writer == nil {
		writer, err = store.NewWriter()
	} else {
		writer, err = s.writer.MoveTo(store)
	}
	if err != nil {
		return errors.Join(err, store.Close(), os.RemoveAll(directory))
	}
	if s.writer != nil {
		if err := s.writer.Abort(); err != nil {
			log.Warn(fmt.Errorf("remove failed cache stage: %w", err))
		}
	}
	s.writer = writer
	s.cacheable = false
	s.temporary = store
	s.directory = directory
	return nil
}

func (s *downloadStage) Write(data []byte) (int, error) {
	count, err := s.writer.Write(data)
	if err == nil || !s.cacheable {
		return count, err
	}
	log.Warn(fmt.Errorf("cache stage write failed, retaining uncached download: %w", err))
	if err := s.fallback(); err != nil {
		return count, err
	}
	rest, err := s.writer.Write(data[count:])
	return count + rest, err
}

func (s *downloadStage) seal() (cas.Object, error) {
	object, err := s.writer.Seal(nil)
	if err == nil || !s.cacheable {
		return object, err
	}
	log.Warn(fmt.Errorf("cache stage sync failed, retaining uncached download: %w", err))
	if err := s.fallback(); err != nil {
		return cas.Object{}, err
	}
	return s.writer.Seal(nil)
}

func (s *downloadStage) Close() error {
	result := s.writer.Abort()
	if s.temporary != nil {
		result = errors.Join(result, s.temporary.Close(), os.RemoveAll(s.directory))
	}
	return result
}
