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

package snappy

import (
	. "gopkg.in/check.v1"
)

type UtilsTestSuite struct{}

var _ = Suite(&UtilsTestSuite{})

func (ts *UtilsTestSuite) TestGetattr(c *C) {
	T := struct {
		S string
		I int
	}{
		S: "foo",
		I: 42,
	}
	// works on values
	c.Assert(getattr(T, "S").(string), Equals, "foo")
	c.Assert(getattr(T, "I").(int), Equals, 42)
	// works for pointers too
	c.Assert(getattr(&T, "S").(string), Equals, "foo")
	c.Assert(getattr(&T, "I").(int), Equals, 42)
}
