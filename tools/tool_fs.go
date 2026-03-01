package tools

import (
	"bufio"
	"io"
	"os"
)

func MoveFile(src *os.File, dest string) (err error) {
	err = os.Rename(src.Name(), dest)
	return
}

func CopyFile(src *os.File, dest string) (file *os.File, err error) {
	destFile, err := os.Create(dest)
	if err != nil {
		return nil, err
	}
	defer func(destFile *os.File) {
		_ = destFile.Close()
	}(destFile)

	_, err = io.Copy(destFile, src)
	if err != nil {
		return nil, err
	}

	return destFile, nil
}

func MoveReaderToLine(r io.Reader, line string) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if scanner.Text() == line {
			return nil
		}
	}
	return scanner.Err()
}
