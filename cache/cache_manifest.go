package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
)

const (
	manifestFilename = "cache.json"
)

type cacheItem struct {
	Filename   string    `json:"filename"`
	Size       int       `json:"size"`
	Sha1       string    `json:"sha1"`
	Expiration time.Time `json:"expiration"`
	Key        key       `json:"key"`
}

type key string

type manifest struct {
	LifeTime time.Duration     `json:"life_time"`
	MaxSize  int               `json:"max_size"`
	Content  map[key]cacheItem `json:"content"`
}

func readManifest(filepath string) *manifest {
	file, err := os.Open(filepath)
	if errors.Is(err, os.ErrNotExist) {
		return createManifest(filepath)
	} else if err != nil {
		return nil
	}
	defer tools.CloseReader(file, logger.Warn)

	data, err := io.ReadAll(file)
	if err != nil {
		// Cannot read the file, delete and create a new one
		_ = resetCache(filepath)
		return createManifest(filepath)
	}

	m := &manifest{}
	err = json.Unmarshal(data, m)
	if err != nil {
		// Cannot unmarshal the manifest, delete and create a new one
		_ = resetCache(filepath)
		return createManifest(filepath)
	}

	// Check if the manifest is valid
	if m.Content == nil || m.LifeTime <= 0 || m.MaxSize <= 0 {
		_ = resetCache(filepath)
		return createManifest(filepath)
	}

	return m
}

func createManifest(filepath string) *manifest {
	// make directory
	dir := path.Dir(filepath)
	if err := os.MkdirAll(dir, os.ModeDir|os.ModePerm); err != nil {
		logger.Warn(
			fmt.Errorf(
				"failed to create manifest directory %s: %w",
				dir, err,
			),
		)
		return nil
	}

	// create manifest file
	file, err := os.Create(filepath)
	if errors.Is(err, os.ErrExist) {
		return readManifest(filepath)
	} else if err != nil {
		logger.Warn(
			fmt.Errorf(
				"failed to create manifest file %s: %w",
				filepath,
				err,
			),
		)
		return nil
	}
	_ = file.Close()

	// new object in memory
	m := &manifest{
		LifeTime: defaultLifeTime,
		MaxSize:  maxSize,
		Content:  make(map[key]cacheItem),
	}

	// write to file
	if err := updateManifest(filepath, m); err != nil {
		logger.Warn(
			fmt.Errorf(
				"failed to write new manifest to %s: %w",
				filepath, err,
			),
		)
		return nil
	}

	return m
}

func updateManifest(filepath string, manifest *manifest) (err error) {
	// check the manifest
	if manifest == nil || manifest.Content == nil {
		return errors.New("invalid manifest: nil or empty content")
	}

	// create a tmp file for atomic write
	tempFile := filepath + ".tmp"

	// marshal the manifest to JSON
	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	// write to the tmp file
	err = os.WriteFile(tempFile, data, 0o600)
	if err != nil {
		// remove the tmp file if write failed
		logger.Warn(os.Remove(tempFile))
		return fmt.Errorf("failed to write temporary manifest file: %w", err)
	}

	// atomic replacement
	err = os.Rename(tempFile, filepath)
	if err != nil {
		// remove the tmp file if replacement failed
		logger.Warn(os.Remove(tempFile))
		return fmt.Errorf("failed to write to manifest file: %w", err)
	}

	return nil
}

func resetCache(manifestPath string) error {
	// remove the manifest file
	err := os.Remove(manifestPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// get the cache's directory (parent directory of the manifest file)
	cacheDir := path.Dir(manifestPath)

	// read
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return err
	}

	// remove all cache items

	// clear all cache
	for _, entry := range entries {
		// just for safety, skip the manifest file
		if entry.Name() == manifestFilename {
			continue
		}

		entryPath := path.Join(cacheDir, entry.Name())
		err := os.RemoveAll(entryPath)
		if err != nil {
			logger.Warn(
				fmt.Errorf(
					"failed to remove cache item %s: %w",
					entryPath, err,
				),
			)
		}
	}

	return nil
}
