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

package userd_test

import (
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/usersession/userd"
)

type helpersSuite struct{}

var _ = Suite(&helpersSuite{})

func (s *helpersSuite) TestSnapFromPidHappy(c *C) {
	restore := userd.MockProcGroup(func(pid int, sel cgroup.GroupSelector) (string, error) {
		c.Assert(pid, Equals, 333)
		c.Assert(sel, DeepEquals, cgroup.GroupSelector{Controller: "freezer"})
		return "/snap.hello-world", nil
	})
	defer restore()
	snap, err := userd.SnapFromPid(333)
	c.Assert(err, IsNil)
	c.Check(snap, Equals, "hello-world")
}

func (s *helpersSuite) TestSnapFromPidUnhappy(c *C) {
	restore := userd.MockProcGroup(func(pid int, sel cgroup.GroupSelector) (string, error) {
		c.Assert(pid, Equals, 333)
		c.Assert(sel, DeepEquals, cgroup.GroupSelector{Controller: "freezer"})
		return "", errors.New("nada")
	})
	defer restore()
	snap, err := userd.SnapFromPid(333)
	c.Assert(err, ErrorMatches, "cannot determine cgroup path of pid 333: nada")
	c.Check(snap, Equals, "")
}
