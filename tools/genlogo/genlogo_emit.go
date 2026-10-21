package main

import (
	"fmt"
	"image/color"
	"strings"
)

// plainText renders the art without escape sequences. It keeps leading
// spaces, which position the glyphs, and drops trailing ones.
func plainText(a art) string {
	var sb strings.Builder
	for row := range a.rows {
		end := rowEnd(a, row)
		for col := range end {
			sb.WriteByte(a.at(col, row).glyph)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// ansiText renders the art with 24-bit SGR foreground colors. It emits a
// color only where it changes and resets at the end of each row, so every
// line is self-contained.
func ansiText(a art) string {
	var sb strings.Builder
	for row := range a.rows {
		end := rowEnd(a, row)
		var current *color.NRGBA
		tinted := false
		for col := range end {
			cell := a.at(col, row)
			if cell.glyph == ' ' {
				sb.WriteByte(' ')
				continue
			}
			switch {
			case cell.tinted && (current == nil || *current != cell.color):
				fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm", cell.color.R, cell.color.G, cell.color.B)
				c := cell.color
				current = &c
				tinted = true
			case !cell.tinted && current != nil:
				sb.WriteString("\x1b[39m")
				current = nil
			}
			sb.WriteByte(cell.glyph)
		}
		if tinted {
			sb.WriteString("\x1b[0m")
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func rowEnd(a art, row int) int {
	end := a.cols
	for end > 0 && a.at(end-1, row).glyph == ' ' {
		end--
	}
	return end
}
