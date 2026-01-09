// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package clientutil_test

import (
	"os"
	"path"
	"sort"

	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

type sessionSuite struct {
	testutil.BaseTest
}

var _ = Suite(&sessionSuite{})

func (s *sessionSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func (s *sessionSuite) TestAvailableUserSessionsHappy(c *C) {
	// fake two sockets, one for 0 and one for 1000
	err := os.MkdirAll(path.Join(dirs.XdgRuntimeDirBase, "0", "snapd-session-agent.socket"), 0o700)
	c.Assert(err, IsNil)
	err = os.MkdirAll(path.Join(dirs.XdgRuntimeDirBase, "1000", "snapd-session-agent.socket"), 0o700)
	c.Assert(err, IsNil)
	err = os.MkdirAll(path.Join(dirs.XdgRuntimeDirBase, "1337", "snapd-session-agent.socket"), 0o700)
	c.Assert(err, IsNil)

	res, err := clientutil.AvailableUserSessions()
	c.Assert(err, IsNil)
	c.Check(res, HasLen, 3)
	sort.Ints(res)
	c.Check(res, DeepEquals, []int{
		0,
		1000,
		1337,
	})
}

func (s *sessionSuite) TestAvailableUserSessionsIgnoresBadUids(c *C) {
	// fake two sockets, one for 0 and one for 1000
	err := os.MkdirAll(path.Join(dirs.XdgRuntimeDirBase, "hello", "snapd-session-agent.socket"), 0o700)
	c.Assert(err, IsNil)
	err = os.MkdirAll(path.Join(dirs.XdgRuntimeDirBase, "*34i8932", "snapd-session-agent.socket"), 0o700)
	c.Assert(err, IsNil)
	err = os.MkdirAll(path.Join(dirs.XdgRuntimeDirBase, "3", "snapd-session-agent.socket"), 0o700)
	c.Assert(err, IsNil)

	res, err := clientutil.AvailableUserSessions()
	c.Assert(err, IsNil)
	c.Check(res, HasLen, 1)
	sort.Ints(res)
	c.Check(res, DeepEquals, []int{
		3,
	})
}
