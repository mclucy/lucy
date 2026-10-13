package cache

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/log"
)

const (
	manifestFilename = "cache.json"
)

type key string

// ResetReport summarizes a cache reset. Removed lists the paths of the
// items the reset actually removed; a reset that does not request
// collection leaves it nil.
type ResetReport struct {
	TotalFreedSize int64
	FileCount      int
	Removed        []string
}

func resetCache(manifestPath string, verbose bool) (ResetReport, error) {
	err := os.Remove(manifestPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ResetReport{}, err
	}

	cacheDir := filepath.Dir(manifestPath)
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return ResetReport{}, err
	}

	var resetResult ResetReport
	for _, entry := range entries {
		if entry.Name() == manifestFilename {
			continue
		}
		entryPath := filepath.Join(cacheDir, entry.Name())

		var size int64
		size, err = calculateSize(entryPath)
		if err != nil {
			log.Debug(
				fmt.Sprintf(
					"failed to calculate size for %s: %v",
					entryPath,
					err,
				),
			)
			size = 0
		}

		if err := os.RemoveAll(entryPath); err != nil {
			log.Warn(
				fmt.Errorf(
					"failed to remove cache item %s: %w",
					entryPath, err,
				),
			)
		} else if verbose {
			resetResult.Removed = append(resetResult.Removed, entryPath)
		}

		resetResult.TotalFreedSize += size
		resetResult.FileCount++
	}

	return resetResult, nil
}

func calculateSize(filePath string) (int64, error) {
	var totalSize int64

	err := filepath.Walk(
		filePath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				totalSize += info.Size()
			}
			return nil
		},
	)

	return totalSize, err
}
