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
	"github.com/snapcore/snapd/render"
)

// HBox is a container that packs items horizontally.
//
// After packing all the elements have the same Y coordinate and have
// consecutive X coordinates according to the size of their predecessor and the
// spacing between them.
type HBox struct {
	rect
	Items   []render.Widget
	Spacing int
}

// Pack arranges all the items horizontally.
//
// The height of the box is the height of tallest item. The width is the sum of
// widths of all the items, with spacing in between. Each item is given the
// remaining width to pack itself.
//
// Spacing is inserted between consecutive items. For typical text applications
// spacing should be set to one to have one column of space between elements.
func (hbox *HBox) Pack(widthAvailable int) {
	widthSoFar := 0
	maxHeight := 0
	for i, item := range hbox.Items {
		if i > 0 {
			widthSoFar += hbox.Spacing
		}
		item.Move(render.Point{X: widthSoFar, Y: 0})
		item.Pack(widthAvailable - widthSoFar)
		widthSoFar += item.Size().Width
		if h := item.Size().Height; h > maxHeight {
			maxHeight = h
		}
	}
	for _, item := range hbox.Items {
		item.Resize(render.Size{Width: item.Size().Width, Height: maxHeight})
	}
	hbox.Resize(render.Size{Width: widthSoFar, Height: maxHeight})
}

func (hbox *HBox) Render() []render.Stripe {
	var stripes []render.Stripe
	for _, item := range hbox.Items {
		stripes = append(stripes, item.Render()...)
	}
	for i := range stripes {
		stripes[i].X += hbox.rect.topLeft.X
		stripes[i].Y += hbox.rect.topLeft.Y
	}
	return stripes
}
