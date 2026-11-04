// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

type levenshteinSuite struct{}

var _ = Suite(&levenshteinSuite{})

func (s *levenshteinSuite) TestLevenshteinDistance(c *C) {
	for _, tc := range []struct {
		a, b string
		d    int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"install", "install", 0},
		{"instal", "install", 1},
		{"install", "instal", 1},
		{"kitten", "sitting", 3},
		{"debug", "debgu", 2},
		{"żółw", "zolw", 3},
	} {
		comment := Commentf("%q vs %q", tc.a, tc.b)
		c.Check(strutil.LevenshteinDistance(tc.a, tc.b), Equals, tc.d, comment)
		// distance is symmetric
		c.Check(strutil.LevenshteinDistance(tc.b, tc.a), Equals, tc.d, comment)
	}
}
