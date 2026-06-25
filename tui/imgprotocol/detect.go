package imgprotocol

import (
	"bytes"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
)

const (
	xtversionQuery        = "\x1b[>q"
	xtversionPrefix       = "\x1b[>q"
	stringTerminator      = "\x1b\\"
	xtversionReadTimeout  = 50 * time.Millisecond
	xtversionDrainTimeout = 100 * time.Millisecond
	maxXTVERSIONBytes     = 256
)

func detect() Protocol {
	if !term.IsTerminal(os.Stdout.Fd()) {
		return None
	}

	protocol, ok := protocolFromEnv()
	if ok {
		return protocol
	}

	return queryXTVERSION()
}

func protocolFromEnv() (Protocol, bool) {
	termProgram := os.Getenv("TERM_PROGRAM")
	switch termProgram {
	case "iTerm.app":
		return Iterm2, true
	case "WezTerm":
		return Iterm2, true
	case "ghostty":
		return Kitty, true
	}

	if os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("TERM") == "xterm-kitty" {
		return Kitty, true
	}

	if os.Getenv("WEZTERM_EXECUTABLE") != "" && termProgram == "" {
		if protocol := queryXTVERSION(); protocol != None {
			return protocol, true
		}
		return Iterm2, true
	}

	return None, false
}

func queryXTVERSION() Protocol {
	if runtime.GOOS == "windows" {
		return None
	}

	oldState, err := term.MakeRaw(os.Stdin.Fd())
	if err != nil {
		return None
	}
	defer func() {
		_ = term.Restore(os.Stdin.Fd(), oldState)
	}()

	if _, err := os.Stdout.Write([]byte(xtversionQuery)); err != nil {
		return None
	}
	_ = os.Stdout.Sync()

	response := readXTVERSIONResponse(xtversionReadTimeout)
	drainStdin(xtversionDrainTimeout)
	return parseXTVERSIONResponse(response)
}

func readXTVERSIONResponse(timeout time.Duration) []byte {
	if err := os.Stdin.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil
	}
	defer func() {
		_ = os.Stdin.SetReadDeadline(time.Time{})
	}()

	resultCh := make(chan []byte, 1)
	go func() {
		resultCh <- readStringResponse(maxXTVERSIONBytes)
	}()

	timer := time.NewTimer(timeout + 10*time.Millisecond)
	defer timer.Stop()

	select {
	case result := <-resultCh:
		return result
	case <-timer.C:
		return nil
	}
}

func readStringResponse(limit int) []byte {
	buf := make([]byte, 0, limit)
	one := make([]byte, 1)
	for len(buf) < limit {
		n, err := os.Stdin.Read(one)
		if n > 0 {
			buf = append(buf, one[0])
			if bytes.HasSuffix(buf, []byte(stringTerminator)) {
				return append([]byte(nil), buf...)
			}
		}
		if err != nil {
			return nil
		}
	}
	return nil
}

func drainStdin(timeout time.Duration) {
	if err := os.Stdin.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return
	}
	defer func() {
		_ = os.Stdin.SetReadDeadline(time.Time{})
	}()

	buf := make([]byte, 64)
	for {
		if _, err := os.Stdin.Read(buf); err != nil {
			return
		}
	}
}

func parseXTVERSIONResponse(data []byte) Protocol {
	start := bytes.Index(data, []byte(xtversionPrefix))
	if start < 0 {
		return None
	}

	payload := data[start+len(xtversionPrefix):]
	if end := bytes.Index(payload, []byte(stringTerminator)); end >= 0 {
		payload = payload[:end]
	}

	name := strings.TrimSpace(strings.TrimPrefix(string(payload), ";"))
	if before, _, found := strings.Cut(name, ";"); found {
		name = before
	}
	return protocolFromTerminalName(name)
}

func protocolFromTerminalName(name string) Protocol {
	lowerName := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.HasPrefix(lowerName, "kitty"):
		return Kitty
	case strings.HasPrefix(lowerName, "wez"):
		return Iterm2
	case strings.HasPrefix(lowerName, "ghostty"):
		return Kitty
	case strings.HasPrefix(lowerName, "iterm2") || lowerName == "iterm.app":
		return Iterm2
	default:
		return None
	}
}
