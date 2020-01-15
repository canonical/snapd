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

package widgets_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/render"
	"github.com/snapcore/snapd/render/widgets"
)

type padSuite struct{}

var _ = Suite(&padSuite{})

// Resizing and moving
func (s *padSuite) TestResizingAndMoving(c *C) {
	pad := widgets.Padding{}
	c.Check(pad.Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(pad.Size(), Equals, render.Size{Width: 0, Height: 0})

	pad.Move(render.Point{X: 10, Y: 5})
	c.Check(pad.Position(), Equals, render.Point{X: 10, Y: 5})

	pad.Resize(render.Size{Width: 3, Height: 7})
	c.Check(pad.Size(), Equals, render.Size{Width: 3, Height: 7})
}

// Packing a pre gives it the size combined of padding and the widget inside.
func (s *padSuite) TestPacking(c *C) {
	pad := widgets.Padding{}
	pad.Pack(80)
	c.Check(pad.Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(pad.Size(), Equals, render.Size{Width: 0, Height: 0})

	pad.Top = 1
	pad.Pack(80)
	c.Check(pad.Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(pad.Size(), Equals, render.Size{Width: 0, Height: 1})

	pad.Left = 1
	pad.Pack(80)
	c.Check(pad.Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(pad.Size(), Equals, render.Size{Width: 1, Height: 1})

	pad.Bottom = 1
	pad.Pack(80)
	c.Check(pad.Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(pad.Size(), Equals, render.Size{Width: 1, Height: 2})

	pad.Right = 1
	pad.Pack(80)
	c.Check(pad.Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(pad.Size(), Equals, render.Size{Width: 2, Height: 2})

	pad.Body = &widgets.Pre{Text: "a"}
	pad.Pack(80)
	c.Check(pad.Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(pad.Size(), Equals, render.Size{Width: 3, Height: 3})
	c.Check(pad.Body.Position(), Equals, render.Point{X: 1, Y: 1})
	c.Check(pad.Body.Size(), Equals, render.Size{Width: 1, Height: 1})
}

// Pres don't re-flow text and are clipped once out of space.
func (s *padSuite) TestPackingInsufficientSpace(c *C) {
	pad := widgets.Padding{Left: 5, Right: 5}
	pad.Pack(7)
	c.Check(pad.Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(pad.Size(), Equals, render.Size{Width: 7, Height: 0})
}

func (s *padSuite) TestRendering(c *C) {
	pad := widgets.Padding{Top: 1, Left: 1, Right: 1, Bottom: 1}
	pad.Pack(80)
	c.Check(pad.Render(), HasLen, 0)

	pad.Body = &widgets.Pre{Text: "a"}
	pad.Pack(80)
	c.Check(pad.Render(), DeepEquals, []render.Stripe{
		{X: 1, Y: 1, ScanLine: "a"},
	})
}

func (s *padSuite) TestClipping(c *C) {
	pad := widgets.Padding{Top: 1, Left: 1, Right: 1, Bottom: 1}
	pad.Body = &widgets.Pre{Text: "0000\n1111\n2222\n3333"}
	pad.Pack(80)
	pad.Resize(render.Size{Width: 3, Height: 3})
	c.Check(pad.Render(), DeepEquals, []render.Stripe{
		{X: 1, Y: 1, ScanLine: "0"},
	})
}
