// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"unicode"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
)

type escapeSuite struct{}

var _ = check.Suite(escapeSuite{})

func (escapeSuite) TestClean(c *check.C) {
	for r := rune(0); r <= unicode.MaxRune; r++ {
		// skip the surrogates as they'er messy
		s := strutil.UnsafeString([]rune{'a', r, 'z'})
		notWanted := r == '�' || unicode.In(r,
			unicode.C,
			unicode.Noncharacter_Code_Point,
		)
		var expected string
		if notWanted {
			expected = "az"
		} else {
			expected = string(s)
		}
		c.Check(s.Clean(), check.Equals, expected, check.Commentf("%t: %#U", notWanted, r))
	}
}
