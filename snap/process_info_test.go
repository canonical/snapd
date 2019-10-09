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

package snap_test

import (
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/snap"
)

type processInfoSuite struct{}

var _ = Suite(&processInfoSuite{})

func (s *processInfoSuite) TestSnapFromPidHappy(c *C) {
	restore := snap.MockProcGroup(func(pid int, matcher cgroup.GroupMatcher) (string, error) {
		c.Assert(pid, Equals, 333)
		c.Assert(matcher, NotNil)
		c.Assert(matcher.String(), Equals, `controller "freezer"`)
		return "/snap.hello-world", nil
	})
	defer restore()
	snap, err := snap.NameFromPid(333)
	c.Assert(err, IsNil)
	c.Check(snap, Equals, "hello-world")
}

func (s *processInfoSuite) TestSnapFromPidUnhappy(c *C) {
	restore := snap.MockProcGroup(func(pid int, matcher cgroup.GroupMatcher) (string, error) {
		c.Assert(pid, Equals, 333)
		c.Assert(matcher, NotNil)
		c.Assert(matcher.String(), Equals, `controller "freezer"`)
		return "", errors.New("nada")
	})
	defer restore()
	snap, err := snap.NameFromPid(333)
	c.Assert(err, ErrorMatches, "cannot determine cgroup path of pid 333: nada")
	c.Check(snap, Equals, "")
}
