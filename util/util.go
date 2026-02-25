// Package util is a general package for network and file system operations.
package util

import (
	"crypto/sha3"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
)

const (
	ProgramPath = ".lucy"
	ConfigFile  = ProgramPath + "/config.json"
)

// DownloadFileWithCache downloads a file from the given URL and saves it to the specified directory.
//
// It calls cache.Network for cache retrieval and storage.
func DownloadFileWithCache(
	url string,
	dir string,
	expiration time.Duration,
) (file *os.File, hit bool, err error) {
	if cache.Network.Exist(url) {
		_, cache, err := cache.Network.Get(url)
		if err != nil {
			return nil, false, err
		}
		file, err = tools.CopyFile(
			cache,
			path.Join(dir, path.Base(cache.Name())),
		)
		if err != nil {
			return nil, false, err
		}
		return file, true, err
	}

	file, data, err := DownloadFile(url, dir)
	if err != nil {
		return nil, false, err
	}
	err = cache.Network.Add(data, file.Name(), url, expiration)
	if err != nil {
		logger.Warn(fmt.Errorf("failed to add file to cache: %w", err))
	}
	return file, false, nil
}

// DownloadFile downloads a file WITHOUT caching (either checking or storing).
func DownloadFile(url string, dir string) (
	file *os.File,
	data []byte,
	err error,
) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, nil, err
	}
	data, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	filename := speculateFilename(resp)
	if filename == "" {
		filename = fmt.Sprintf("%x", sha3.Sum256(data))
	}
	file, err = os.Create(path.Join(dir, filename))
	if err != nil {
		return nil, nil, err
	}
	_, err = file.Write(data)
	if err != nil {
		return nil, nil, err
	}
	return file, data, nil
}

func speculateFilename(resp *http.Response) string {
	if filename, ok := getFilenameFromHeader(resp); ok {
		return filename
	}
	filename := getFilenameFromURL(resp.Request.URL.String())
	return filename
}

func getFilenameFromHeader(resp *http.Response) (string, bool) {
	contentDisposition := resp.Header.Get("Content-Disposition")
	if contentDisposition == "" {
		return "", false
	}

	_, params, err := mime.ParseMediaType(contentDisposition)
	if err != nil {
		return "", false
	}

	filename, ok := params["filename"]
	return filename, ok
}

func getFilenameFromURL(urlString string) string {
	u, err := url.Parse(urlString)
	if err != nil {
		return ""
	}

	segments := strings.Split(u.Path, "/")
	if len(segments) == 0 {
		return ""
	}

	filename := segments[len(segments)-1]

	return filename
}
