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

package main

import (
	. "gopkg.in/check.v1"
)

func (s *CmdTestSuite) TestParseSetPropertyCmdline(c *C) {

	// simple case
	pkgname, args, err := parseSetPropertyCmdline("hello-world", "channel=edge")
	c.Assert(err, IsNil)
	c.Assert(pkgname, Equals, "hello-world")
	c.Assert(args, DeepEquals, []string{"channel=edge"})

	// special case, see spec
	// ensure that just "active=$ver" uses "ubuntu-core" as the pkg
	pkgname, args, err = parseSetPropertyCmdline("channel=alpha")
	c.Assert(err, IsNil)
	c.Assert(pkgname, Equals, "ubuntu-core")
	c.Assert(args, DeepEquals, []string{"channel=alpha"})
}
