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

type preSuite struct{}

var _ = Suite(&preSuite{})

// Resizing and moving
func (s *preSuite) TestResizingAndMoving(c *C) {
	pre := widgets.Pre{}
	c.Check(pre.Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(pre.Size(), Equals, render.Size{Width: 0, Height: 0})

	pre.Move(render.Point{X: 10, Y: 5})
	c.Check(pre.Position(), Equals, render.Point{X: 10, Y: 5})

	pre.Resize(render.Size{Width: 3, Height: 7})
	c.Check(pre.Size(), Equals, render.Size{Width: 3, Height: 7})
}

// Packing a pre gives it the size of the text inside.
func (s *preSuite) TestPacking(c *C) {
	pre := widgets.Pre{Text: "123\n123456\n1234567"}
	pre.Pack(80)
	c.Check(pre.Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(pre.Size(), Equals, render.Size{Width: 7, Height: 3})
}

// Pres don't re-flow text and are clipped once out of space.
func (s *preSuite) TestPackingInsufficientSpace(c *C) {
	pre := widgets.Pre{Text: "Hello World"}
	pre.Pack(7)
	c.Check(pre.Position(), Equals, render.Point{X: 0, Y: 0})
	c.Check(pre.Size(), Equals, render.Size{Width: 7, Height: 1})
}

func (s *preSuite) TestTextRendering(c *C) {
	pre := widgets.Pre{}
	pre.Pack(80)
	c.Check(pre.Render(), DeepEquals, []render.Stripe{
		{X: 0, Y: 0, ScanLine: ""},
	})

	pre = widgets.Pre{Text: "pre"}
	pre.Pack(80)
	c.Check(pre.Render(), DeepEquals, []render.Stripe{
		{X: 0, Y: 0, ScanLine: "pre"},
	})

	pre = widgets.Pre{Text: "some text\nand more\n\n"}
	pre.Pack(80)
	c.Check(pre.Render(), DeepEquals, []render.Stripe{
		{X: 0, Y: 0, ScanLine: "some text"},
		{X: 0, Y: 1, ScanLine: "and more"},
		{X: 0, Y: 2, ScanLine: ""},
		{X: 0, Y: 3, ScanLine: ""},
	})

	pre = widgets.Pre{Text: "\n\nspeak yoda\nwe can"}
	pre.Pack(80)
	c.Check(pre.Render(), DeepEquals, []render.Stripe{
		{X: 0, Y: 0, ScanLine: ""},
		{X: 0, Y: 1, ScanLine: ""},
		{X: 0, Y: 2, ScanLine: "speak yoda"},
		{X: 0, Y: 3, ScanLine: "we can"},
	})
}

func (s *preSuite) TestHorizontalAlignment(c *C) {
	pre := widgets.Pre{Text: "a"}
	pre.Resize(render.Size{Width: 10, Height: 10})

	pre.HAlign = widgets.Beginning
	c.Check(pre.Render(), DeepEquals, []render.Stripe{
		{X: 0, Y: 0, ScanLine: "a"},
	})

	pre.HAlign = widgets.Center
	c.Check(pre.Render(), DeepEquals, []render.Stripe{
		{X: 4, Y: 0, ScanLine: "a"},
	})

	pre.HAlign = widgets.End
	c.Check(pre.Render(), DeepEquals, []render.Stripe{
		{X: 9, Y: 0, ScanLine: "a"},
	})
}

func (s *preSuite) TestVerticalAlignment(c *C) {
	pre := widgets.Pre{Text: "a"}
	pre.Resize(render.Size{Width: 10, Height: 10})

	pre.VAlign = widgets.Beginning
	c.Check(pre.Render(), DeepEquals, []render.Stripe{
		{X: 0, Y: 0, ScanLine: "a"},
	})

	pre.VAlign = widgets.Center
	c.Check(pre.Render(), DeepEquals, []render.Stripe{
		{X: 0, Y: 4, ScanLine: "a"},
	})

	pre.VAlign = widgets.End
	c.Check(pre.Render(), DeepEquals, []render.Stripe{
		{X: 0, Y: 9, ScanLine: "a"},
	})
}

func (s *preSuite) TestClipping(c *C) {
	pre := widgets.Pre{Text: "0000\n1111\n2222\n3333"}
	pre.Pack(80)
	pre.Resize(render.Size{Width: 3, Height: 3})
	c.Check(pre.Render(), DeepEquals, []render.Stripe{
		{X: 0, Y: 0, ScanLine: "000"},
		{X: 0, Y: 1, ScanLine: "111"},
		{X: 0, Y: 2, ScanLine: "222"},
	})
}
