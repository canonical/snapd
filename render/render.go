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

	// Using a one-line buffer render each each line, consuming stripes in
	// order. This bounds the complexity of the render algorithm to O(N) +
	// O(Nlog(N)).
	lineBuffer := make([][]rune, maxX)
	for i := range lineBuffer {
		lineBuffer[i] = make([]rune, 1)
	}
	sIdx := 0 // index to the current stripe we are considering.
	sEnd := len(stripes)
	for y := 0; y <= maxY; y += 1 {
		// Reset the line buffer.
		for i := 0; i < maxX; i++ {
			if len(lineBuffer[i]) == 0 {
				lineBuffer[i] = append(lineBuffer[i], ' ')
			} else {
				lineBuffer[i][0] = ' '
			}
		}
		// Advance to the first stripe for the line we want to render.
		for ; sIdx < sEnd; sIdx++ {
			if stripes[sIdx].Y >= y {
				break
			}
		}
		// Composite all stripes for current Y index.
		for ; sIdx < sEnd; sIdx++ {
			if stripes[sIdx].Y != y {
				break
			}
			x := stripes[sIdx].X
			for _, r := range stripes[sIdx].ScanLine {
				switch heuristic.RuneWidth(r) {
				case 0:
					lineBuffer[x] = lineBuffer[x][:0]
				case 1:
					if len(lineBuffer[x]) == 0 {
						lineBuffer[x] = append(lineBuffer[x], r)
					} else {
						lineBuffer[x][0] = r
					}
					x++
				case 2:
					if len(lineBuffer[x]) == 0 {
						lineBuffer[x] = append(lineBuffer[x], r)
					} else {
						lineBuffer[x][0] = r
					}
					lineBuffer[x+1] = lineBuffer[x+1][:0]
					x += 2
				}
			}
		}

		// Strip trailing white-space to avoid printing a big rectangle.
		var right int
		for right = maxX - 1; right >= 0; right-- {
			if rb := lineBuffer[right]; len(rb) > 0 && rb[0] != ' ' {
				break
			}
		}
		utf8Buffer := make([]byte, 16)
		for i := 0; i <= right; i++ {
			if runes := lineBuffer[i]; len(runes) > 0 {
				n := utf8.EncodeRune(utf8Buffer, runes[0])
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
