package imgprotocol

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/jpeg"
	"image/png"
	"strconv"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/iterm2"
	"github.com/charmbracelet/x/ansi/sixel"
)

type dimensions struct {
	width  int
	height int
}

func render(protocol Protocol, data []byte, size dimensions) string {
	if len(data) == 0 {
		return ""
	}

	switch protocol {
	case Kitty:
		return renderKitty(data, size)
	case Iterm2:
		return renderIterm2(data, size)
	case Sixel:
		return renderSixel(data)
	default:
		return ""
	}
}

func renderKitty(data []byte, size dimensions) string {
	payload, ok := pngPayload(data)
	if !ok {
		return ""
	}

	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(payload)))
	base64.StdEncoding.Encode(encoded, payload)

	opts := []string{"a=T", "t=d", "f=100", "q=2"}
	if size.width > 0 {
		opts = append(opts, "c="+strconv.Itoa(size.width))
	}
	if size.height > 0 {
		opts = append(opts, "r="+strconv.Itoa(size.height))
	}

	return ansi.KittyGraphics(encoded, opts...)
}

func renderIterm2(data []byte, size dimensions) string {
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(data)))
	base64.StdEncoding.Encode(encoded, data)

	file := iterm2.File{
		Size:    int64(len(data)),
		Inline:  true,
		Content: encoded,
	}
	if size.width > 0 {
		file.Width = iterm2.Cells(size.width)
	}
	if size.height > 0 {
		file.Height = iterm2.Cells(size.height)
	}

	return ansi.ITerm2(file)
}

func renderSixel(data []byte) string {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ""
	}

	var payload bytes.Buffer
	var encoder sixel.Encoder
	if err := encoder.Encode(&payload, img); err != nil {
		return ""
	}

	return ansi.SixelGraphics(0, 1, 0, payload.Bytes())
}

func pngPayload(data []byte) ([]byte, bool) {
	if bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		return data, true
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, false
	}

	var payload bytes.Buffer
	if err := png.Encode(&payload, img); err != nil {
		return nil, false
	}
	return payload.Bytes(), true
}
