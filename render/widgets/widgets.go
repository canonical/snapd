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

// Alignment determines to position content within a rectangle.
type Alignment int

const (
	// Beginning positions the content at the beginning of the available space.
	Beginning Alignment = iota
	// Center positions the content in the center of the available space.
	Center
	// End positions the content at the end of the available space.
	End
)

type rect struct {
	topLeft render.Point
	size    render.Size
}

func (r *rect) Position() render.Point {
	return r.topLeft
}

func (r *rect) Move(to render.Point) {
	r.topLeft = to
}

func (r *rect) Size() render.Size {
	return r.size
}

func (r *rect) Resize(to render.Size) {
	r.size = to
}
