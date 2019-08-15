// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package osutil_test

import (
	"os/user"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type findUserGroupSuite struct {
	testutil.BaseTest
}

var _ = check.Suite(&findUserGroupSuite{})

func (s *findUserGroupSuite) TestFindUid(c *check.C) {
	uid, err := osutil.FindUid("root")
	c.Assert(err, check.IsNil)
	c.Assert(uid, check.Equals, uint64(0))
}

func (s *findUserGroupSuite) TestFindUidNonexistent(c *check.C) {
	_, err := osutil.FindUid("lakatos")
	c.Assert(err, check.ErrorMatches, "user: unknown user lakatos")
	_, ok := err.(user.UnknownUserError)
	c.Assert(ok, check.Equals, true)
}

func (s *findUserGroupSuite) TestFindGid(c *check.C) {
	gid, err := osutil.FindGid("root")
	c.Assert(err, check.IsNil)
	c.Assert(gid, check.Equals, uint64(0))
}

func (s *findUserGroupSuite) TestFindGidNonexistent(c *check.C) {
	_, err := osutil.FindGid("lakatos")
	c.Assert(err, check.ErrorMatches, "group: unknown group lakatos")
	_, ok := err.(user.UnknownGroupError)
	c.Assert(ok, check.Equals, true)
}
