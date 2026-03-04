package cache

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"github.com/mclucy/lucy/logger"
)

const (
	manifestFilename = "cache.json"
)

type key string

type ResetReport struct {
	TotalFreedSize int64
	FileCount      int
}

func resetCache(manifestPath string, verbose bool) (ResetReport, error) {
	err := os.Remove(manifestPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ResetReport{}, err
	}

	cacheDir := path.Dir(manifestPath)
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return ResetReport{}, err
	}

	var report ResetReport
	for _, entry := range entries {
		if entry.Name() == manifestFilename {
			continue
		}
		entryPath := path.Join(cacheDir, entry.Name())

		var size int64
		size, err = calculateSize(entryPath)
		if err != nil {
			logger.Debug(
				fmt.Sprintf(
					"failed to calculate size for %s: %v",
					entryPath,
					err,
				),
			)
			size = 0
		}

		if err := os.RemoveAll(entryPath); err != nil {
			logger.Warn(
				fmt.Errorf(
					"failed to remove cache item %s: %w",
					entryPath, err,
				),
			)
		} else if verbose {
			logger.ShowInfo(fmt.Sprintf("removed %s", entryPath))
		}

		report.TotalFreedSize += size
		report.FileCount++
	}

	return report, nil
}

// calculateSize recursively calculates the total size of a file or directory
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
