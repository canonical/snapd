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

package naming_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/naming"
)

type nameSuite struct{}

var _ = Suite(&nameSuite{})

func (s *nameSuite) TestSnapNameString(c *C) {
	c.Check(naming.SnapName("foo").String(), Equals, "foo")
	c.Check(naming.SnapName("").String(), Equals, "")
}

func (s *nameSuite) TestInstanceNameString(c *C) {
	c.Check(naming.InstanceName("foo").String(), Equals, "foo")
	c.Check(naming.InstanceName("foo_bar").String(), Equals, "foo_bar")
}

func (s *nameSuite) TestNewInstanceName(c *C) {
	c.Check(naming.NewInstanceName("foo", ""), Equals, naming.InstanceName("foo"))
	c.Check(naming.NewInstanceName("foo", "bar"), Equals, naming.InstanceName("foo_bar"))
}

func (s *nameSuite) TestInstanceNameSnapName(c *C) {
	// plain instance name
	c.Check(naming.InstanceName("foo").SnapName(), Equals, naming.SnapName("foo"))
	// parallel install instance name
	c.Check(naming.InstanceName("foo_bar").SnapName(), Equals, naming.SnapName("foo"))
}

func (s *nameSuite) TestInstanceNameInstanceKey(c *C) {
	// plain instance name has no instance key
	c.Check(naming.InstanceName("foo").InstanceKey(), Equals, "")
	// parallel install instance name
	c.Check(naming.InstanceName("foo_bar").InstanceKey(), Equals, "bar")
}

func (s *nameSuite) TestInstanceNameRoundTrip(c *C) {
	for _, in := range []naming.InstanceName{"foo", "foo_bar"} {
		c.Check(naming.NewInstanceName(in.SnapName(), in.InstanceKey()), Equals, in)
	}
}
