package imgprotocol

import "sync"

// Protocol identifies a terminal image protocol.
type Protocol int

const (
	None Protocol = iota
	Kitty
	Iterm2
	Sixel
)

var (
	detectOnce     sync.Once
	detectedResult Protocol
)

// String returns the protocol name.
func (p Protocol) String() string {
	switch p {
	case Kitty:
		return "kitty"
	case Iterm2:
		return "iterm2"
	case Sixel:
		return "sixel"
	default:
		return "none"
	}
}

// Detect determines the image protocol supported by the current terminal.
func Detect() Protocol {
	detectOnce.Do(
		func() {
			detectedResult = detect()
		},
	)
	return detectedResult
}

// Render renders image data using the detected protocol.
func Render(data []byte, width, height int) string {
	return render(Detect(), data, dimensions{width: width, height: height})
}
