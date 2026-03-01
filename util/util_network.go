package util

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
)

// MultiSourceDownload expects the urls hosts the same file. However, it does
// not verify the checksums to allow more loose file recognition policies in its
// callers.
//
// Download is concurrent. Other threads will be cancelled when one thread
// complete downloaded winThreshold of the file.
//
// Note that if the urls' speed are close, urls[0] will be selected since its
// goroutine is started first.
//
// Pros:
//   - Guaranteed to download the file from the fastest source.
//
// Cons:
//   - Wastes bandwidth
func MultiSourceDownload(urls []string, path string) {
	const winThreshold = 0.2 // 20% of the file
	var wg sync.WaitGroup
	var mu sync.Mutex
	var win bool
	var data *[]byte
	var winUrl string

	for _, url := range urls {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			resp, err := http.Get(url)
			if err != nil {
				return
			}
			defer resp.Body.Close()

			// TODO: totalSize might be -1 when size it not known, handle this case
			totalSize := resp.ContentLength
			thresholdSize := int64(float64(totalSize) * winThreshold)
			buffer := make([]byte, 2048)
			var downloadedSize int64

			for {
				n, err := resp.Body.Read(buffer)
				if err != nil && err != io.EOF {
					return
				}
				if n == 0 {
					break
				}
				downloadedSize += int64(n)
				if win && winUrl != url {
					println(
						"canceling:",
						url,
						"("+strconv.FormatInt(downloadedSize, 10)+"/"+
							strconv.FormatInt(totalSize, 10), "bytes)",
					)
					return
				}
				if downloadedSize >= thresholdSize {
					mu.Lock()
					if !win {
						println(
							"winning:",
							url,
							"("+strconv.FormatInt(downloadedSize, 10)+"/"+
								strconv.FormatInt(totalSize, 10), "bytes)",
						)
						win = true
						data = &buffer
						winUrl = url
					}
					mu.Unlock()
				}
			}
		}(url)
	}

	wg.Wait()
	println("winning url: ", winUrl)

	file, _ := os.Create(path)
	defer func(file *os.File) {
		_ = file.Close()
	}(file)
	_, err := file.Write(*data)
	if err != nil {
		panic(err)
	}

	println("Downloaded to", path)
}
