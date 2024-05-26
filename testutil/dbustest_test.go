// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package testutil_test

import (
	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/testutil"
)

type dbusSuite struct {
	testutil.DBusTest
}

var _ = Suite(&dbusSuite{})

func (s *dbusSuite) TestSessionBus(c *C) {
	var names []string
	mylog.Check(s.SessionBus.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names))

	// Only two connections to the bus: the bus itself, and the test process
	c.Check(names, HasLen, 2)
}

func (s *dbusSuite) TestNoActivatableNames(c *C) {
	// The private session bus does not expose activatable
	// services from the system the test suite is running on.
	var names []string
	mylog.Check(s.SessionBus.BusObject().Call("org.freedesktop.DBus.ListActivatableNames", 0).Store(&names))

	c.Check(names, DeepEquals, []string{
		"org.freedesktop.DBus",
	})
}
