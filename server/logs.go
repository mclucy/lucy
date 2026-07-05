package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"time"
)

func StreamLog(ctx context.Context, path string, follow bool, out io.Writer) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open log %s: %w", path, err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			if _, writeErr := io.WriteString(out, line); writeErr != nil {
				return writeErr
			}
		}
		if err == nil {
			continue
		}
		if err != io.EOF {
			return err
		}
		if !follow {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(500 * time.Millisecond):
		}
	}
}
