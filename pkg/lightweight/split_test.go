// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package lightweight

import (
	"gopkg.in/check.v1"
)

type splitSuite struct{}

var _ = check.Suite(&splitSuite{})

func (s *splitSuite) TestSplitRegular(c *check.C) {
	for _, s := range []string{"meh/foo.bar/baz", "foo.bar/baz"} {
		n, e, f := split(s)
		c.Check(n, check.Equals, "foo")
		c.Check(e, check.Equals, "bar")
		c.Check(f, check.Equals, "baz")
	}
}

func (s *splitSuite) TestSplitExtless(c *check.C) {
	for _, s := range []string{"meh/foo/baz", "foo/baz"} {
		n, e, f := split(s)
		c.Check(n, check.Equals, "foo")
		c.Check(e, check.Equals, "")
		c.Check(f, check.Equals, "baz")
	}
}

func (s *splitSuite) TestSplitBad(c *check.C) {
	c.Check(func() {
		split("what")
	}, check.PanicMatches, `bad path given.*`)
}

func (s *splitSuite) TestExtract(c *check.C) {
	ps := []string{"meh/foo.bar/v1", "meh/foo.bar/v2", "meh/foo.baz/v3"}
	n, o, vs, ps := extract(ps)
	c.Check(n, check.Equals, "foo")
	c.Check(o, check.Equals, "bar")
	c.Check(vs, check.DeepEquals, []string{"v1", "v2"})
	c.Check(ps, check.DeepEquals, []string{"meh/foo.baz/v3"})
}
