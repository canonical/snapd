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

package widgets

import (
	"strings"

	"github.com/snapcore/snapd/render"
	"github.com/snapcore/snapd/render/heuristic"
)

// Pre is a widget that displays pre-formatted text.
//
// The text is never re-flowed but can be aligned both horizontally and
// vertically within the available space that the widget occupies.
type Pre struct {
	rect

	Text   string
	VAlign Alignment
	HAlign Alignment
}

func (pre *Pre) Pack(widthAvailable int) {
	w, h := heuristic.TerminalRenderSize(pre.Text)
	// Clip the pre if it doesn't fit.
	if w > widthAvailable {
		w = widthAvailable
	}
	pre.Resize(render.Size{Width: w, Height: h})
}

// Render produces a strip for each visible line in the pre.
func (pre *Pre) Render() []render.Stripe {
	// XXX: Render should handle more control characters, presumably by using
	// heuristic to tokenize text into actionable elements, to reuse the single
	// definition of all the heuristic.
	lines := strings.Split(pre.Text, "\n")
	stripes := make([]render.Stripe, 0, len(lines))
	for i, line := range lines {
		if i >= pre.rect.size.Height {
			break
		}
		w := 0
		h := 1
		for i, r := range line {
			if inc := heuristic.RuneWidth(r); w+inc <= pre.rect.size.Width {
				w += inc
			} else {
				line = line[:i]
				break
			}
		}
		var x, y int
		switch pre.HAlign {
		case Beginning:
			x = 0
		case Center:
			x = (pre.rect.size.Width - w) / 2
		case End:
			x = pre.rect.size.Width - w
		}
		switch pre.VAlign {
		case Beginning:
			y = i
		case Center:
			y = (pre.rect.size.Height - (h + i)) / 2
		case End:
			y = pre.rect.size.Height - (h + i)
		}
		stripes = append(stripes, render.Stripe{
			X:        pre.rect.topLeft.X + x,
			Y:        pre.rect.topLeft.Y + y,
			ScanLine: line,
		})
	}
	return stripes
}
