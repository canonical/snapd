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

type hboxSuite struct{}

var _ = Suite(&hboxSuite{})

// HBox that is empty takes no space.
func (s *hboxSuite) TestPackingEmpty(c *C) {
	hbox := widgets.HBox{}
	hbox.Pack(80)
	c.Check(hbox.Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(hbox.Size(), Equals, render.Size{Width: 0, Height: 0})
}

// HBox with some labels inside is packed as consecutive columns.
func (s *hboxSuite) TestPackingTypical(c *C) {
	hbox := widgets.HBox{
		Items: []render.Widget{
			&widgets.Pre{Text: "123"},
			&widgets.Pre{Text: "ab"},
		},
		Spacing: 1,
	}
	// Move to (10, 10) to see how position of the box is reflected in the items.
	hbox.Move(render.Point{X: 10, Y: 10})
	hbox.Pack(80)
	c.Check(hbox.Position(), Equals, render.Point{X: 10, Y: 10})
	// Nested widgets use relative positioning and are unaffected by the position of the box.
	c.Check(hbox.Items[0].Size(), Equals, render.Size{Width: 3, Height: 1})
	c.Check(hbox.Items[1].Size(), Equals, render.Size{Width: 2, Height: 1})
	c.Check(hbox.Items[0].Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(hbox.Items[1].Position(), Equals, render.Point{X: 4, Y: 0})
	c.Check(hbox.Size(), Equals, render.Size{Width: 3 + 2 + /* spacing */ 1, Height: 1})
}

// Packing a hbox sets the height of all items to that of the tallest one.
func (s *hboxSuite) TestPackingCommonHeight(c *C) {
	hbox := widgets.HBox{
		Items: []render.Widget{
			&widgets.Pre{Text: "hello"},
			&widgets.Pre{Text: "\n-\n"},
			&widgets.Pre{Text: "horizontal"},
		},
		Spacing: 1,
	}
	hbox.Pack(80)
	c.Check(hbox.Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(hbox.Items[0].Size(), Equals, render.Size{Width: 5, Height: 3})
	c.Check(hbox.Items[1].Size(), Equals, render.Size{Width: 1, Height: 3})
	c.Check(hbox.Items[2].Size(), Equals, render.Size{Width: 10, Height: 3})
	c.Check(hbox.Items[0].Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(hbox.Items[1].Position(), Equals, render.Point{X: 6, Y: 0})
	c.Check(hbox.Items[2].Position(), Equals, render.Point{X: 8, Y: 0})
	c.Check(hbox.Size(), Equals, render.Size{Width: 5 + 1 + 10 + /* spacing */ 2, Height: 3})
}

func (s *hboxSuite) TestRendering(c *C) {
	hbox := widgets.HBox{
		Items: []render.Widget{
			&widgets.Pre{Text: "123"},
			&widgets.Pre{Text: "ab"},
		},
		Spacing: 1,
	}
	hbox.Pack(80)
	c.Check(hbox.Render(), DeepEquals, []render.Stripe{
		{X: 0, Y: 0, ScanLine: "123"},
		{X: 4, Y: 0, ScanLine: "ab"},
	})
}
