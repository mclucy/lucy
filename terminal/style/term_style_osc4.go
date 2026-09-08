package style

import (
	"bufio"
	"bytes"
	"fmt"
	"image/color"
	"io"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/muesli/termenv"
	"golang.org/x/term"
)

func osc4Query(index uint8) color.Color {
	if index > 15 {
		return nil
	}

	profile := termenv.ColorProfile()
	if !shouldQueryOSC4(profile) {
		return nil
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil
	}
	defer tty.Close()

	fd := int(tty.Fd())
	if !term.IsTerminal(fd) {
		return nil
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil
	}
	defer func() { _ = term.Restore(fd, oldState) }()

	// OSC 4 query. ST is the official string terminator; many terminals
	// also accept BEL, but ST is used here.
	query := fmt.Sprintf("\x1b]4;%d;?\x1b\\", index)

	if _, err := tty.Write([]byte(query)); err != nil {
		return nil
	}

	const timeout = 100 * time.Millisecond
	deadline := time.Now().Add(timeout)
	_ = tty.SetReadDeadline(deadline)

	resp := readResponseWithTimeout(bufio.NewReader(tty), timeout, tty)
	if resp == nil {
		return nil
	}
	return parseOSC4Response(index, resp)
}

func readResponseWithTimeout(
	r *bufio.Reader,
	timeout time.Duration,
	closer io.Closer,
) []byte {
	resultCh := make(chan []byte, 1)
	go func() {
		var buf bytes.Buffer
		for {
			b, err := r.ReadByte()
			if err != nil {
				resultCh <- nil
				return
			}
			buf.WriteByte(b)

			data := buf.Bytes()
			n := len(data)
			if b == '\a' || (n >= 2 && data[n-2] == 0x1b && data[n-1] == '\\') {
				out := append([]byte(nil), data...)
				resultCh <- out
				return
			}

			if buf.Len() > 4096 {
				resultCh <- nil
				return
			}
		}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result := <-resultCh:
		return result
	case <-timer.C:
		if closer != nil {
			_ = closer.Close()
		}
		return nil
	}
}

func shouldQueryOSC4(profile termenv.Profile) bool {
	return profile == termenv.TrueColor
}

func parseOSC4Response(index uint8, data []byte) color.Color {
	// Response: ESC ] 4 ; <i> ; rgb:rrrr/gggg/bbbb ESC \. Some terminals
	// return "#"-hex; only rgb:.... is handled here.
	re := regexp.MustCompile(`\x1b\]4;` + strconv.Itoa(int(index)) + `;rgb:([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})`)
	m := re.FindSubmatch(data)
	if m == nil {
		prefix := []byte("\x1b]4;" + strconv.Itoa(int(index)) + ";")
		if bytes.Contains(data, prefix) {
			return nil
		}
		return nil
	}

	r16, _ := strconv.ParseUint(string(m[1]), 16, 16)
	g16, _ := strconv.ParseUint(string(m[2]), 16, 16)
	b16, _ := strconv.ParseUint(string(m[3]), 16, 16)
	scale := func(v uint64) uint8 { return uint8((v * 255) / 65535) }

	return color.RGBA{
		R: scale(r16),
		G: scale(g16),
		B: scale(b16),
		A: 255,
	}
}
