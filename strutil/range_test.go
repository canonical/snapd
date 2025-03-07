// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package strutil_test

import (
	"github.com/snapcore/snapd/strutil"
	. "gopkg.in/check.v1"
)

type rangeSuite struct{}

var _ = Suite(&rangeSuite{})

func (s *rangeSuite) TestParseRange(c *C) {
	r, err := strutil.ParseRange("3,2,-100--20,-1")
	c.Assert(err, IsNil)

	c.Check(r.Spans[0], Equals, strutil.RangeSpan{3, 3})
	c.Check(r.Spans[1], Equals, strutil.RangeSpan{2, 2})
	c.Check(r.Spans[2], Equals, strutil.RangeSpan{-100, -20})
	c.Check(r.Spans[3], Equals, strutil.RangeSpan{-1, -1})

	c.Check(r.Size(), Equals, 84)
	c.Check(r.Intersets(strutil.RangeSpan{0, 5}), Equals, true)
	c.Check(r.Intersets(strutil.RangeSpan{-101, -100}), Equals, true)
	c.Check(r.Intersets(strutil.RangeSpan{5, 5}), Equals, false)
}

func (s *rangeSuite) TestParseRangeError(c *C) {
	_, err := strutil.ParseRange(" 1")
	c.Assert(err, ErrorMatches, `strconv.ParseInt: parsing " 1": invalid syntax`)

	_, err = strutil.ParseRange("1,-1-2")
	c.Assert(err, ErrorMatches, `overlapping range span found "-1-2"`)

	_, err = strutil.ParseRange("1-")
	c.Assert(err, ErrorMatches, `invalid range "1-": .*`)

	_, err = strutil.ParseRange("a-2")
	c.Assert(err, ErrorMatches, `invalid range "a-2": .*`)

	_, err = strutil.ParseRange("1--2")
	c.Assert(err, ErrorMatches, `invalid range "1--2": range end has to be larger than range start`)
}
