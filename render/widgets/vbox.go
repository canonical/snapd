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

// VBox is a container that packs items vertically.
//
// After packing all the elements have the same X coordinate and have
// consecutive Y coordinates according to the size of their predecessor and the
// spacing between them.
type VBox struct {
	rect
	Items   []render.Widget
	Spacing int
}

// Pack arranges all the items vertically.
//
// The width of the box is the width of widest item. The height is the sum of
// heights of all the items, with spacing in between. Each item is given the
// entire width available to pack itself.
//
// Spacing is inserted between consecutive items. For typical text applications
// spacing should be set to zero to have line-by-line output.
func (vbox *VBox) Pack(widthAvailable int) {
	x := vbox.rect.topLeft.X
	y := vbox.rect.topLeft.Y
	maxWidth := 0
	heightSoFar := 0
	for i, item := range vbox.Items {
		if i > 0 {
			heightSoFar += vbox.Spacing
		}
		item.Move(render.Point{X: x, Y: y + heightSoFar})
		item.Pack(widthAvailable)
		if w := item.Size().Width; w > maxWidth {
			maxWidth = w
		}
		heightSoFar += item.Size().Height
	}
	for _, item := range vbox.Items {
		item.Resize(render.Size{Width: maxWidth, Height: item.Size().Height})
	}
	vbox.Resize(render.Size{Width: maxWidth, Height: heightSoFar})
}

func (vbox *VBox) Render() []render.Stripe {
	var stripes []render.Stripe
	for _, item := range vbox.Items {
		stripes = append(stripes, item.Render()...)
	}
	return stripes
}
