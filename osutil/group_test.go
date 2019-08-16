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
	mockGetent *testutil.MockCmd
}

var _ = check.Suite(&findUserGroupSuite{})

func (s *findUserGroupSuite) SetUpTest(c *check.C) {
	// exit 2 is not found
	s.mockGetent = testutil.MockCommand(c, "getent", "exit 2")
}

func (s *findUserGroupSuite) TearDownTest(c *check.C) {
	s.mockGetent.Restore()
}

func (s *findUserGroupSuite) TestFindUid(c *check.C) {
	uid, err := osutil.FindUid("root")
	c.Assert(err, check.IsNil)
	c.Assert(uid, check.Equals, uint64(0))
	// getent shouldn't have been called with FindUid()
	c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string(nil))
}

func (s *findUserGroupSuite) TestFindUidNonexistent(c *check.C) {
	_, err := osutil.FindUid("lakatos")
	c.Assert(err, check.ErrorMatches, "user: unknown user lakatos")
	_, ok := err.(user.UnknownUserError)
	c.Assert(ok, check.Equals, true)
	// getent shouldn't have been called with FindUid()
	c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string(nil))
}

func (s *findUserGroupSuite) TestFindUidGetent(c *check.C) {
	uid, err := osutil.FindUidGetent("root")
	c.Assert(err, check.IsNil)
	c.Assert(uid, check.Equals, uint64(0))
	// getent shouldn't have been called since 'root' is in /etc/passwd
	c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string(nil))
}

func (s *findUserGroupSuite) TestFindUidGetentNonexistent(c *check.C) {
	_, err := osutil.FindUidGetent("lakatos")
	c.Assert(err, check.ErrorMatches, "user: unknown user lakatos")
	_, ok := err.(user.UnknownUserError)
	c.Assert(ok, check.Equals, true)
	// getent should've have been called
	c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string{
		{"getent", "passwd", "lakatos"},
	})
}

func (s *findUserGroupSuite) TestFindUidGetentMockedOtherError(c *check.C) {
	s.mockGetent = testutil.MockCommand(c, "getent", "exit 3")

	uid, err := osutil.FindUidGetent("lakatos")
	c.Assert(err, check.ErrorMatches, "cannot run getent: exit status 3")
	c.Check(uid, check.Equals, uint64(0))
	// getent should've have been called
	c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string{
		{"getent", "passwd", "lakatos"},
	})
}

func (s *findUserGroupSuite) TestFindUidGetentMocked(c *check.C) {
	s.mockGetent = testutil.MockCommand(c, "getent", "echo lakatos:x:1234:5678:::")

	uid, err := osutil.FindUidGetent("lakatos")
	c.Assert(err, check.IsNil)
	c.Check(uid, check.Equals, uint64(1234))
	c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string{
		{"getent", "passwd", "lakatos"},
	})
}

func (s *findUserGroupSuite) TestFindUidGetentMockedMalformated(c *check.C) {
	s.mockGetent = testutil.MockCommand(c, "getent", "printf too:few:colons")

	_, err := osutil.FindUidGetent("lakatos")
	c.Assert(err, check.ErrorMatches, `malformed entry: "too:few:colons"`)
}

func (s *findUserGroupSuite) TestFindGid(c *check.C) {
	uid, err := osutil.FindGid("root")
	c.Assert(err, check.IsNil)
	c.Assert(uid, check.Equals, uint64(0))
	// getent shouldn't have been called with FindGid()
	c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string(nil))
}

func (s *findUserGroupSuite) TestFindGidNonexistent(c *check.C) {
	_, err := osutil.FindGid("lakatos")
	c.Assert(err, check.ErrorMatches, "group: unknown group lakatos")
	_, ok := err.(user.UnknownGroupError)
	c.Assert(ok, check.Equals, true)
	// getent shouldn't have been called with FindGid()
	c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string(nil))
}

func (s *findUserGroupSuite) TestFindGidGetent(c *check.C) {
	uid, err := osutil.FindGidGetent("root")
	c.Assert(err, check.IsNil)
	c.Assert(uid, check.Equals, uint64(0))
	// getent shouldn't have been called since 'root' is in /etc/passwd
	c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string(nil))
}

func (s *findUserGroupSuite) TestFindGidGetentNonexistent(c *check.C) {
	_, err := osutil.FindGidGetent("lakatos")
	c.Assert(err, check.ErrorMatches, "group: unknown group lakatos")
	_, ok := err.(user.UnknownGroupError)
	c.Assert(ok, check.Equals, true)
	// getent should've have been called
	c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string{
		{"getent", "group", "lakatos"},
	})
}

func (s *findUserGroupSuite) TestFindGidGetentMockedOtherError(c *check.C) {
	s.mockGetent = testutil.MockCommand(c, "getent", "exit 3")

	gid, err := osutil.FindGidGetent("lakatos")
	c.Assert(err, check.ErrorMatches, "cannot run getent: exit status 3")
	c.Check(gid, check.Equals, uint64(0))
	// getent should've have been called
	c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string{
		{"getent", "group", "lakatos"},
	})
}

func (s *findUserGroupSuite) TestFindGidGetentMocked(c *check.C) {
	s.mockGetent = testutil.MockCommand(c, "getent", "echo lakatos:x:1234:")

	uid, err := osutil.FindGidGetent("lakatos")
	c.Assert(err, check.IsNil)
	c.Check(uid, check.Equals, uint64(1234))
	c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string{
		{"getent", "group", "lakatos"},
	})
}
