package main

import "image/color"

// art is a glyph grid in row-major order. Every row has cols cells.
type art struct {
	cols, rows int
	cells      []artCell
}

// artCell is one terminal cell. A cell with tinted false uses the terminal's
// default foreground.
type artCell struct {
	glyph  byte
	color  color.NRGBA
	tinted bool
}

func newArt(cols, rows int) art {
	cells := make([]artCell, cols*rows)
	for i := range cells {
		cells[i].glyph = ' '
	}
	return art{cols: cols, rows: rows, cells: cells}
}

func (a art) at(col, row int) artCell {
	return a.cells[row*a.cols+col]
}

func (a art) set(col, row int, c artCell) {
	a.cells[row*a.cols+col] = c
}
