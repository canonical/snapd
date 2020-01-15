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

// Padding applies padding to a nested widget.
type Padding struct {
	rect

	// The amount of padding to apply
	Top, Left, Bottom, Right int
	// The widget to render in the center
	Body render.Widget
}

func (pad *Padding) Pack(widthAvailable int) {
	extraWidth := pad.Left + pad.Right
	extraHeight := pad.Top + pad.Bottom
	size := render.Size{Width: extraWidth, Height: extraHeight}
	if pad.Body != nil {
		pad.Body.Move(render.Point{X: pad.rect.topLeft.X + pad.Left, Y: pad.rect.topLeft.Y + pad.Top})
		pad.Body.Pack(widthAvailable - extraWidth)
		bodySize := pad.Body.Size()
		size.Width += bodySize.Width
		size.Height += bodySize.Height
	}
	// Clip the pre if it doesn't fit.
	if size.Width > widthAvailable {
		size.Width = widthAvailable
	}
	pad.rect.Resize(size)
}

func (pad *Padding) Resize(size render.Size) {
	pad.rect.Resize(size)
	if pad.Body != nil {
		pad.Body.Resize(render.Size{Width: size.Width - pad.Left - pad.Right, Height: size.Height - pad.Top - pad.Bottom})
	}
}

func (pad *Padding) Render() []render.Stripe {
	if pad.Body != nil {
		return pad.Body.Render()
	}
	return nil
}
