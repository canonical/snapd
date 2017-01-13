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
	}{
		{"1.0", "2.0", -1},
		{"1.3", "1.2.2.2", 1},
		{"1.3", "1.3.1", -1},
		{"1.0", "1.0~", 1},
		{"7.2p2", "7.2", 1},
		{"0.4a6", "0.4", 1},
		{"0pre", "0pre", 0},
		{"0pree", "0pre", 1},
		{"1.18.36:5.4", "1.18.36:5.5", -1},
		{"1.18.36:5.4", "1.18.37:1.1", -1},
		{"2.0.7pre1", "2.0.7r", -1},
		{"0.10.0", "0.8.7", 1},
		// subrev
		{"1.0-1", "1.0-2", -1},
		{"1.0-1.1", "1.0-1", 1},
		{"1.0-1.1", "1.0-1.1", 0},
		// do we like strange versions? Yes we like strange versions…
		{"0", "0", 0},
		{"0", "00", 0},
	} {
		res := strutil.VersionCompare(t.A, t.B)
		c.Check(res, Equals, t.res, Commentf("%s %s: %v but got %v", t.A, t.B, res, t.res))
	}
}

func (s *VersionTestSuite) TestVersionInvalid(c *C) {
	for _, t := range []struct {
		ver   string
		valid bool
	}{
		{"1:2", false},
		{"1--1", false},
		{"1.0", true},
	} {
		res := strutil.VersionIsValid(t.ver)
		c.Check(res, Equals, t.valid, Commentf("%q: %v but expected %v", t.ver, res, t.valid))
	}
}
