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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
)

type rangeSuite struct{}

var _ = Suite(&rangeSuite{})

func (s *rangeSuite) TestParseRange(c *C) {
	r, err := strutil.ParseRange("20-100,0,3,2")
	c.Assert(err, IsNil)

	// Parsed and sorted by start
	c.Check(r[0], Equals, strutil.RangeSpan{0, 0})
	c.Check(r[1], Equals, strutil.RangeSpan{2, 2})
	c.Check(r[2], Equals, strutil.RangeSpan{3, 3})
	c.Check(r[3], Equals, strutil.RangeSpan{20, 100})

	c.Check(r.Size(), Equals, 84)
	c.Check(r.Intersects(strutil.RangeSpan{0, 1}), Equals, true)
	c.Check(r.Intersects(strutil.RangeSpan{5, 20}), Equals, true)
	c.Check(r.Intersects(strutil.RangeSpan{5, 5}), Equals, false)
	c.Check(r.String(), Equals, "0,2,3,20-100")
}

func (s *rangeSuite) TestParseRangeError(c *C) {
	_, err := strutil.ParseRange(" 1")
	c.Assert(err, ErrorMatches, `strconv.ParseUint: parsing " 1": invalid syntax`)

	_, err = strutil.ParseRange("1,0-2")
	c.Assert(err, ErrorMatches, `overlapping range span found "0-2"`)

	_, err = strutil.ParseRange("1-3,0-2")
	c.Assert(err, ErrorMatches, `overlapping range span found "0-2"`)

	_, err = strutil.ParseRange("0-2,1-3")
	c.Assert(err, ErrorMatches, `overlapping range span found "1-3"`)

	_, err = strutil.ParseRange("0-2,2-3")
	c.Assert(err, ErrorMatches, `overlapping range span found "2-3"`)

	_, err = strutil.ParseRange("1-")
	c.Assert(err, ErrorMatches, `invalid range span end "1-": .*`)

	_, err = strutil.ParseRange("a-2")
	c.Assert(err, ErrorMatches, `invalid range span start "a-2": .*`)

	_, err = strutil.ParseRange("1--2")
	c.Assert(err, ErrorMatches, `invalid range span end "1--2": strconv.ParseUint: parsing \"-2\": invalid syntax`)

	_, err = strutil.ParseRange("2-1")
	c.Assert(err, ErrorMatches, `invalid range span "2-1": ends before it starts`)
}

func (s *rangeSuite) TestString(c *C) {
	for _, tc := range []struct {
		input, expected string
	}{
		{"2,1", "1,2"},
		{"4,0,2-3,1", "0,1,2-3,4"},
		{"0-10000", "0-10000"},
		{"10", "10"},
	} {
		r, err := strutil.ParseRange(tc.input)
		c.Assert(err, IsNil)
		c.Check(r.String(), Equals, tc.expected, Commentf("input: %q, expected: %q", tc.input, tc.expected))
	}

}
