package fsutil

import (
	"bufio"
	"io"
)

func MoveReaderToLine(r io.Reader, line string) error {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		if scanner.Text() == line {
			return nil
		}
	}
	return scanner.Err()
}
