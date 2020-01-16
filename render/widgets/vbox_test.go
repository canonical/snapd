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

type vboxSuite struct{}

var _ = Suite(&vboxSuite{})

// VBox that is empty takes no space.
func (s *vboxSuite) TestPackingEmpty(c *C) {
	vbox := widgets.VBox{}
	vbox.Pack(80)
	c.Check(vbox.Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(vbox.Size(), Equals, render.Size{Width: 0, Height: 0})
}

// VBox with some labels inside is packed as consecutive lines.
func (s *vboxSuite) TestPackingTypical(c *C) {
	vbox := widgets.VBox{
		Items: []render.Widget{
			&widgets.Pre{Text: "123"},
			&widgets.Pre{Text: "ab"},
		},
	}
	// Move to (10, 10) to see how position of the box is reflected in the items.
	vbox.Move(render.Point{X: 10, Y: 10})
	vbox.Pack(80)
	// Nested widgets use relative positioning and are unaffected by the position of the box.
	c.Check(vbox.Position(), Equals, render.Point{X: 10, Y: 10})
	c.Check(vbox.Items[0].Size(), Equals, render.Size{Width: 3, Height: 1})
	c.Check(vbox.Items[1].Size(), Equals, render.Size{Width: 3, Height: 1})
	c.Check(vbox.Items[0].Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(vbox.Items[1].Position(), Equals, render.Point{X: 0, Y: 1})
	c.Check(vbox.Size(), Equals, render.Size{Width: 3, Height: 2})
}

// Packing a vbox sets the width of all items to that of the widest one.
func (s *vboxSuite) TestPackingCommonWidth(c *C) {
	vbox := widgets.VBox{
		Items: []render.Widget{
			&widgets.Pre{Text: "hello"},
			&widgets.Pre{Text: "-"},
			&widgets.Pre{Text: "vertical"},
		},
	}
	vbox.Pack(80)
	c.Check(vbox.Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(vbox.Items[0].Size(), Equals, render.Size{Width: 8, Height: 1})
	c.Check(vbox.Items[1].Size(), Equals, render.Size{Width: 8, Height: 1})
	c.Check(vbox.Items[2].Size(), Equals, render.Size{Width: 8, Height: 1})
	c.Check(vbox.Items[0].Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(vbox.Items[1].Position(), Equals, render.Point{X: 0, Y: 1})
	c.Check(vbox.Items[2].Position(), Equals, render.Point{X: 0, Y: 2})
	c.Check(vbox.Size(), Equals, render.Size{Width: 8, Height: 3})
}

func (s *vboxSuite) TestRendering(c *C) {
	vbox := widgets.VBox{
		Items: []render.Widget{
			&widgets.Pre{Text: "123"},
			&widgets.Pre{Text: "ab"},
		},
	}
	vbox.Pack(80)
	c.Check(vbox.Render(), DeepEquals, []render.Stripe{
		{X: 0, Y: 0, ScanLine: "123"},
		{X: 0, Y: 1, ScanLine: "ab"},
	})
}
