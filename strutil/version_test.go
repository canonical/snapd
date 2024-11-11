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

package strutil_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
)

type VersionTestSuite struct{}

var _ = Suite(&VersionTestSuite{})

func (s *VersionTestSuite) TestVersionCompare(c *C) {
	for _, t := range []struct {
		A, B string
		res  int
		err  error
	}{
		{"20000000000000000000", "020000000000000000000", 0, nil},
		{"1.0", "2.0", -1, nil},
		{"1.3", "1.2.2.2", 1, nil},
		{"1.3", "1.3.1", -1, nil},
		{"1.0", "1.0~", 1, nil},
		{"7.2p2", "7.2", 1, nil},
		{"0.4a6", "0.4", 1, nil},
		{"0pre", "0pre", 0, nil},
		{"0pree", "0pre", 1, nil},
		{"1.18.36:5.4", "1.18.36:5.5", -1, nil},
		{"1.18.36:5.4", "1.18.37:1.1", -1, nil},
		{"2.0.7pre1", "2.0.7r", -1, nil},
		{"0.10.0", "0.8.7", 1, nil},
		// subrev
		{"1.0-1", "1.0-2", -1, nil},
		{"1.0-1.1", "1.0-1", 1, nil},
		{"1.0-1.1", "1.0-1.1", 0, nil},
		// do we like strange versions? Yes we like strange versions…
		{"0", "0", 0, nil},
		{"0", "00", 0, nil},
		{"", "", 0, nil},
		{"", "0", -1, nil},
		{"0", "", 1, nil},
		{"", "~", 1, nil},
		{"~", "", -1, nil},
		// from the apt suite
		{"0-pre", "0-pre", 0, nil},
		{"0-pre", "0-pree", -1, nil},
		{"1.1.6r2-2", "1.1.6r-1", 1, nil},
		{"2.6b2-1", "2.6b-2", 1, nil},
		{"0.4a6-2", "0.4-1", 1, nil},
		{"3.0~rc1-1", "3.0-1", -1, nil},
		{"1.0", "1.0-0", 0, nil},
		{"0.2", "1.0-0", -1, nil},
		{"1.0", "1.0-0+b1", -1, nil},
		{"1.0", "1.0-0~", 1, nil},
		// from the old perl cupt
		{"1.2.3", "1.2.3", 0, nil},                   // identical
		{"4.4.3-2", "4.4.3-2", 0, nil},               // identical
		{"1.2.3", "1.2.3-0", 0, nil},                 // zero revision
		{"009", "9", 0, nil},                         // zeroes…
		{"009ab5", "9ab5", 0, nil},                   // there as well
		{"1.2.3", "1.2.3-1", -1, nil},                // added non-zero revision
		{"1.2.3", "1.2.4", -1, nil},                  // just bigger
		{"1.2.4", "1.2.3", 1, nil},                   // order doesn't matter
		{"1.2.24", "1.2.3", 1, nil},                  // bigger, eh?
		{"0.10.0", "0.8.7", 1, nil},                  // bigger, eh?
		{"3.2", "2.3", 1, nil},                       // major number rocks
		{"1.3.2a", "1.3.2", 1, nil},                  // letters rock
		{"0.5.0~git", "0.5.0~git2", -1, nil},         // numbers rock
		{"2a", "21", -1, nil},                        // but not in all places
		{"1.2a+~bCd3", "1.2a++", -1, nil},            // tilde doesn't rock
		{"1.2a+~bCd3", "1.2a+~", 1, nil},             // but first is longer!
		{"5.10.0", "5.005", 1, nil},                  // preceding zeroes don't matters
		{"3a9.8", "3.10.2", -1, nil},                 // letters are before all letter symbols
		{"3a9.8", "3~10", 1, nil},                    // but after the tilde
		{"1.4+OOo3.0.0~", "1.4+OOo3.0.0-4", -1, nil}, // another tilde check
		{"2.4.7-1", "2.4.7-z", -1, nil},              // revision comparing
		{"1.002-1+b2", "1.00", 1, nil},               // whatever...
		{"12-20220319-1ubuntu1", "13-1-1", -1, nil},  // two "-" are legal
		{"0--0", "0", 1, nil},                        // also legal (urgh)

		// more realistic example of what we deal with in spread tests
		// where on the left is a version from CI built snapd snap, and
		// on the right the distro package, where snap > package
		{"1337.2.64+git81.g9b95e8c", "1337.2.64", 1, nil},
	} {
		res, err := strutil.VersionCompare(t.A, t.B)
		if t.err != nil {
			c.Check(err, DeepEquals, t.err)
		} else {
			c.Check(err, IsNil)
			c.Check(res, Equals, t.res, Commentf("%#v %#v: %v but got %v", t.A, t.B, res, t.res))
		}
	}
}

func (s *VersionTestSuite) TestVersionInvalid(c *C) {
	for _, t := range []struct {
		ver   string
		valid bool
	}{
		{"1:2", false},
		{"12:34", false},
		{"1234:", false},
		{"1.0", true},
		{"1234", true},
	} {
		res := strutil.VersionIsValid(t.ver)
		c.Check(res, Equals, t.valid, Commentf("%q: %v but expected %v", t.ver, res, t.valid))
	}
}
