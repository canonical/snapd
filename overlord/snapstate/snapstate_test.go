// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package snapstate

import (
	. "gopkg.in/check.v1"
)

type snapstateTestSuite struct{}

var _ = Suite(&snapstateTestSuite{})

func (s *snapstateTestSuite) TestParseSnapSec(c *C) {
	for _, t := range []struct {
		snapSpec string
		name     string
		version  string
	}{
		{"foo", "foo", ""},
		{"foo=2.0", "foo", "2.0"},
		{"foo.mvo", "foo.mvo", ""},
		{"foo.mvo=2.0", "foo.mvo", "2.0"},
	} {

		name, ver := parseSnapSpec(t.snapSpec)
		c.Assert(name, Equals, t.name)
		c.Assert(ver, Equals, t.version)
	}

}
