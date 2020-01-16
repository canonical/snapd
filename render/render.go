// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package render

import (
	"os"
	"sort"
	"unicode/utf8"

	"github.com/snapcore/snapd/render/heuristic"
)

// Point contains the X and Y coordinates.
//
// The coordinate system is such that (0, 0) is the top-left corner of the
// screen/terminal. X axis grows towards the right while the Y axis grows
// towards the bottom.
type Point struct {
	X int
	Y int
}

// Size contains the width and height.
type Size struct {
	Width  int
	Height int
}

// Stripe represents a horizontal stripe to be rendered.
type Stripe struct {
	X, Y     int
	ScanLine string
}

// RenderWidth returns the number of cells the stripe takes after rendering.
func (s *Stripe) RenderWidth() int {
	w, _ := heuristic.TerminalRenderSize(s.ScanLine)
	return w
}

// Widget is anything that can be displayed.
//
// Widgets can be rendered into a number of stripes, perfect for writing to a
// terminal.  Internally one widget can contain other widgets. The way they are
// all positioned given a fixed amount of horizontal space (vertical space is
// assumed to be unconstrained) can be determined by Pack. After that the
// precise location of each widget can be queried by Size and Position.
type Widget interface {
	// Pack computes the size and position of the widget given the available
	// width and its current position. Pack may alter the size of the widget
	// and can resize and move any constituent widgets.
	Pack(widthAvailable int)

	// Render slices the widget into a number of stripes that are easier to
	// print to a stream or display on a terminal.
	Render() []Stripe

	// Position returns the coordinate of the top-left corner of the widget
	// relative to the parent, if any. Relative positioning allows a widget to
	// be re-positioned without affecting its children.
	Position() Point
	// Move re-positions the widget without changing its size.
	Move(to Point)
	// Size returns the width and height of the widget.
	Size() Size
	// Resize reshapes the widget without changing the location of the top-left corner.
	Resize(to Size)
}

// inPaintOrder allows sorting stripes by their (Y, X) coordinates.
type inPaintOrder []Stripe

func (stripes inPaintOrder) Len() int {
	return len(stripes)
}

func (stripes inPaintOrder) Less(i, j int) bool {
	if stripes[i].Y == stripes[j].Y {
		return stripes[i].X < stripes[j].X
	}
	return stripes[i].Y < stripes[j].Y
}

func (stripes inPaintOrder) Swap(i, j int) {
	stripes[i], stripes[j] = stripes[j], stripes[i]
}

// composeStripes composites a number of stripes and prints them to a stream.
func composeStripes(f *os.File, stripes []Stripe) {
	// Sort the stripes in paint order. This allows for single pass printing to
	// the given stream.
	sort.Sort(inPaintOrder(stripes))

	// Find the size of the canvas we are rendering. Maximum Y is easy, it's
	// the Y of the last stripe. Maximum X is more costly since we don't know
	// how many stripes are on the last line.
	var maxX, maxY int
	if n := len(stripes); n > 0 {
		maxY = stripes[n-1].Y
	}
	for _, stripe := range stripes {
		if x := stripe.X + stripe.RenderWidth(); x > maxX {
			maxX = x
		}
	}

	// The rendering algorithm advances along the Y axis and renders all
	// stripes that intersect it, compositing overlapping stripes in the order
	// they are painted.
	//
	// Rendering is performed using a line buffer. The buffer is a array of
	// rune slices. This is necessary for two separate reasons:
	//
	// 1) In principle each cell can contain a single character but encoding of
	// that character may require multiple runes. It might involve using
	// surrogate runes. It might involve (eventually) supporting terminal
	// control sequences that need to be emitted or altered for each cell.
	//
	// 2) Due to the existence of double-width characters we need to pair each
	// double-width character with an empty cell. Empty cells are encoded as an
	// empty slice of runes.
	lineBuffer := make([][]rune, maxX)
	for i := range lineBuffer {
		lineBuffer[i] = make([]rune, 1)
	}
	// Line buffer is rendered, rune by rune, into a utf8 buffer that is also
	// shared across the entire render process.
	utf8Buffer := make([]byte, 16)
	sIdx := 0 // index to the current stripe we are considering.
	sEnd := len(stripes)
	for y := 0; y <= maxY; y += 1 {
		// Reset the line buffer. Each cell is resent to render a single space.
		for i := 0; i < maxX; i++ {
			lineBuffer[i] = append(lineBuffer[i][:0], ' ')
		}
		// Advance to the first stripe for the line we want to render.
		for ; sIdx < sEnd; sIdx++ {
			if stripes[sIdx].Y >= y {
				break
			}
		}
		localMaxX := 0
		// Composite all stripes for current Y index.
		for ; sIdx < sEnd && stripes[sIdx].Y == y; sIdx++ {
			x := stripes[sIdx].X
			// Walk over the runes of the stripe we are rendering.
			for _, r := range stripes[sIdx].ScanLine {
				switch heuristic.RuneWidth(r) {
				case 0:
					// Writing a zero-width rune does nothing at all (no operation takes place).
				case 1:
					// Writing a single-width rune stores it into the current
					// cell. The cell may be empty or may contain a rune that
					// gets overwritten. If the overwritten rune is double
					// width then automatically put a space in the next cell to
					// preserve the correct width.
					switch cell := lineBuffer[x]; len(cell) {
					case 0:
						lineBuffer[x] = append(cell, r)
					case 1:
						if heuristic.RuneWidth(cell[0]) == 2 && x+1 < maxX {
							lineBuffer[x+1] = append(lineBuffer[x+1][:0], ' ')
						}
						cell[0] = r
					}
					// Writing a single-width rune may require us to erase a
					// double-width rune at a previous cell.
					if x > 0 {
						if prevCell := lineBuffer[x-1]; len(prevCell) > 0 && heuristic.RuneWidth(prevCell[0]) == 2 {
							lineBuffer[x-1] = append(prevCell[:0], ' ')
						}
					}
					x++
				case 2:
					// Writing a single-width rune stores it into the current cell. The cell may be
					// empty or may contain a rune that can be overwritten.
					switch cell := lineBuffer[x]; len(cell) {
					case 0:
						lineBuffer[x] = append(cell, r)
					case 1:
						cell[0] = r
					}
					// Writing a double-width rune always writes an empty spot
					// at a next cell. This automatically erases any
					// single-width rune that may have been stored there.
					if x+1 < maxX {
						if nextCell := lineBuffer[x+1]; len(nextCell) > 0 {
							lineBuffer[x+1] = nextCell[:0]
						}
					}
					x += 2
				}
			}
			if x > localMaxX {
				localMaxX = x
			}
		}

		// Scan the line buffer and print each cell that we wrote to.
		// All runes are encoded into UTF-8 and written to stream.
		for i := 0; i < localMaxX; i++ {
			for _, r := range lineBuffer[i] {
				n := utf8.EncodeRune(utf8Buffer, r)
				f.Write(utf8Buffer[:n])
			}
		}
		f.Write([]byte{'\n'})
	}
}

func Display(f *os.File, w Widget) {
	w.Pack(80) // XXX: can we guess that from stream?
	composeStripes(f, w.Render())
}
