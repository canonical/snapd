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
	"fmt"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/user"
	"github.com/snapcore/snapd/testutil"
)

type findUserGroupSuite struct {
	testutil.BaseTest
	mockGetent *testutil.MockCmd
}

var _ = check.Suite(&findUserGroupSuite{})

func (s *findUserGroupSuite) SetUpTest(c *check.C) {
	// exit 2 is not found
	if user.GetentBased {
		s.mockGetent = testutil.MockCommand(c, "getent", `
if [ "${1}" == "passwd" ] && [ "${2}" == "root" ]; then
  echo 'root:x:0:0:root:/root:/bin/bash'
  exit 0
fi
if [ "${1}" == "group" ] && [ "${2}" == "root" ]; then
  echo 'root:x:0:'
  exit 0
fi
exit 2`)
	} else {
		s.mockGetent = testutil.MockCommand(c, "getent", `
exit 2`)
	}

	user.OverrideGetentSearchPath("/foo:/bar:" + s.mockGetent.BinDir())
	s.AddCleanup(func() {
		user.OverrideGetentSearchPath(user.DefaultGetentSearchPath)
	})
}

func (s *findUserGroupSuite) TearDownTest(c *check.C) {
	s.mockGetent.Restore()
}

func (s *findUserGroupSuite) TestFindUidNoGetentFallback(c *check.C) {
	uid, err := osutil.FindUidNoGetentFallback("root")
	c.Assert(err, check.IsNil)
	c.Assert(uid, check.Equals, uint64(0))
	if !user.GetentBased {
		// getent shouldn't have been called with FindUidNoGetentFallback()
		c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string(nil))
	}
}

func (s *findUserGroupSuite) TestFindUidNonexistent(c *check.C) {
	_, err := osutil.FindUidNoGetentFallback("lakatos")
	c.Assert(err, check.ErrorMatches, "user: unknown user lakatos")
	_, ok := err.(user.UnknownUserError)
	c.Assert(ok, check.Equals, true)
	if user.GetentBased {
		c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string{
			{"getent", "passwd", "lakatos"},
		})
	} else {
		// getent shouldn't have been called with FindUidNoGetentFallback()
		c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string(nil))
	}
}

func (s *findUserGroupSuite) TestFindUidWithGetentFallback(c *check.C) {
	uid, err := osutil.FindUidWithGetentFallback("root")
	c.Assert(err, check.IsNil)
	c.Assert(uid, check.Equals, uint64(0))
	if !user.GetentBased {
		// getent shouldn't have been called since 'root' is in /etc/passwd
		c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string(nil))
	}
}

func (s *findUserGroupSuite) TestFindUidGetentNonexistent(c *check.C) {
	_, err := osutil.FindUidWithGetentFallback("lakatos")
	c.Assert(err, check.ErrorMatches, "user: unknown user lakatos")
	_, ok := err.(user.UnknownUserError)
	c.Assert(ok, check.Equals, true)
	if user.GetentBased {
		// getent should've have been called
		c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string{
			{"getent", "passwd", "lakatos"},
			{"getent", "passwd", "lakatos"},
		})
	} else {
		c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string{
			{"getent", "passwd", "lakatos"},
		})
	}
}

func (s *findUserGroupSuite) TestFindUidGetentFoundFromGetent(c *check.C) {
	restore := osutil.MockFindUidNoFallback(func(string) (uint64, error) {
		return 1000, nil
	})
	defer restore()

	uid, err := osutil.FindUidWithGetentFallback("some-user")
	c.Assert(err, check.IsNil)
	c.Assert(uid, check.Equals, uint64(1000))
	// getent not called, "some-user" was available in the local db
	c.Check(s.mockGetent.Calls(), check.HasLen, 0)
}

func (s *findUserGroupSuite) TestFindUidGetentOtherErrFromFindUid(c *check.C) {
	restore := osutil.MockFindUidNoFallback(func(string) (uint64, error) {
		return 0, fmt.Errorf("other-error")
	})
	defer restore()

	_, err := osutil.FindUidWithGetentFallback("root")
	c.Assert(err, check.ErrorMatches, "other-error")
}

func (s *findUserGroupSuite) TestFindUidGetentMockedOtherError(c *check.C) {
	s.mockGetent = testutil.MockCommand(c, "getent", "exit 3")
	// clean up is done in TearDownTest
	user.OverrideGetentSearchPath(s.mockGetent.BinDir())

	uid, err := osutil.FindUidWithGetentFallback("lakatos")
	c.Assert(err, check.ErrorMatches, "getent failed with: exit status 3")
	c.Check(uid, check.Equals, uint64(0))
	if user.GetentBased {
		// getent should've have been called
		c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string{
			{"getent", "passwd", "lakatos"},
			{"getent", "passwd", "lakatos"},
		})
	} else {
		// getent should've have been called
		c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string{
			{"getent", "passwd", "lakatos"},
		})
	}
}

func (s *findUserGroupSuite) TestFindUidGetentMocked(c *check.C) {
	s.mockGetent = testutil.MockCommand(c, "getent", "echo lakatos:x:1234:5678:::")

	uid, err := osutil.FindUidWithGetentFallback("lakatos")
	c.Assert(err, check.IsNil)
	c.Check(uid, check.Equals, uint64(1234))
	c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string{
		{"getent", "passwd", "lakatos"},
	})
}

func (s *findUserGroupSuite) TestFindUidGetentMockedMalformated(c *check.C) {
	s.mockGetent = testutil.MockCommand(c, "getent", "printf too:few:colons")

	_, err := osutil.FindUidWithGetentFallback("lakatos")
	c.Assert(err, check.ErrorMatches, `malformed entry: "too:few:colons"`)
}

func (s *findUserGroupSuite) TestFindGidNoGetentFallback(c *check.C) {
	gid, err := osutil.FindGidNoGetentFallback("root")
	if !user.GetentBased {
		c.Assert(err, check.IsNil)
		c.Assert(gid, check.Equals, uint64(0))
		// getent shouldn't have been called with FindGidNoGetentFallback()
		c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string(nil))
	}
}

func (s *findUserGroupSuite) TestFindGidNonexistent(c *check.C) {
	_, err := osutil.FindGidNoGetentFallback("lakatos")
	c.Assert(err, check.ErrorMatches, "group: unknown group lakatos")
	_, ok := err.(user.UnknownGroupError)
	c.Assert(ok, check.Equals, true)
}

func (s *findUserGroupSuite) TestFindGidGetentFoundFromGetent(c *check.C) {
	restore := osutil.MockFindGidNoFallback(func(string) (uint64, error) {
		return 1000, nil
	})
	defer restore()

	gid, err := osutil.FindGidWithGetentFallback("some-group")
	c.Assert(err, check.IsNil)
	c.Assert(gid, check.Equals, uint64(1000))
	// getent not called, "some-group" was available in the local db
	c.Check(s.mockGetent.Calls(), check.HasLen, 0)
}

func (s *findUserGroupSuite) TestFindGidGetentOtherErrFromFindUid(c *check.C) {
	restore := osutil.MockFindGidNoFallback(func(string) (uint64, error) {
		return 0, fmt.Errorf("other-error")
	})
	defer restore()

	_, err := osutil.FindGidWithGetentFallback("root")
	c.Assert(err, check.ErrorMatches, "other-error")
}

func (s *findUserGroupSuite) TestFindGidWithGetentFallback(c *check.C) {
	gid, err := osutil.FindGidWithGetentFallback("root")
	c.Assert(err, check.IsNil)
	c.Assert(gid, check.Equals, uint64(0))
	if !user.GetentBased {
		// getent shouldn't have been called since 'root' is in /etc/group
		c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string(nil))
	}
}

func (s *findUserGroupSuite) TestFindGidGetentNonexistent(c *check.C) {
	_, err := osutil.FindGidWithGetentFallback("lakatos")
	c.Assert(err, check.ErrorMatches, "group: unknown group lakatos")
	_, ok := err.(user.UnknownGroupError)
	c.Assert(ok, check.Equals, true)
	if user.GetentBased {
		// getent should've have been called
		c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string{
			{"getent", "group", "lakatos"},
			{"getent", "group", "lakatos"},
		})
	} else {
		// getent should've have been called
		c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string{
			{"getent", "group", "lakatos"},
		})
	}
}

func (s *findUserGroupSuite) TestFindGidGetentMockedOtherError(c *check.C) {
	s.mockGetent = testutil.MockCommand(c, "getent", "exit 3")
	// clean up is done in TearDownTest
	user.OverrideGetentSearchPath(s.mockGetent.BinDir())

	gid, err := osutil.FindGidWithGetentFallback("lakatos")
	c.Assert(err, check.ErrorMatches, "getent failed with: exit status 3")
	c.Check(gid, check.Equals, uint64(0))
	// getent should've have been called
	if user.GetentBased {
		c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string{
			{"getent", "group", "lakatos"},
			{"getent", "group", "lakatos"},
		})
	} else {
		c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string{
			{"getent", "group", "lakatos"},
		})
	}
}

func (s *findUserGroupSuite) TestFindGidGetentMocked(c *check.C) {
	s.mockGetent = testutil.MockCommand(c, "getent", "echo lakatos:x:1234:")
	// clean up is done in TearDownTest
	user.OverrideGetentSearchPath(s.mockGetent.BinDir())

	gid, err := osutil.FindGidWithGetentFallback("lakatos")
	c.Assert(err, check.IsNil)
	c.Check(gid, check.Equals, uint64(1234))
	c.Check(s.mockGetent.Calls(), check.DeepEquals, [][]string{
		{"getent", "group", "lakatos"},
	})
}

func (s *findUserGroupSuite) TestFindNoGetentMocked(c *check.C) {
	// clean up is done in TearDownTest
	user.OverrideGetentSearchPath("/foo:/bar")

	uid, err := osutil.FindUidWithGetentFallback("lakatos")
	c.Assert(err, check.ErrorMatches, "user: unknown user lakatos")
	c.Check(uid, check.Equals, uint64(0))
}

func (s *findUserGroupSuite) TestIsUnknownUser(c *check.C) {
	c.Check(osutil.IsUnknownUser(nil), check.Equals, false)
	c.Check(osutil.IsUnknownUser(fmt.Errorf("something else")), check.Equals, false)
	c.Check(osutil.IsUnknownUser(user.UnknownUserError("lakatos")), check.Equals, true)
}

func (s *findUserGroupSuite) TestIsUnknownGroup(c *check.C) {
	c.Check(osutil.IsUnknownGroup(nil), check.Equals, false)
	c.Check(osutil.IsUnknownGroup(fmt.Errorf("something else")), check.Equals, false)
	c.Check(osutil.IsUnknownGroup(user.UnknownGroupError("lakatos")), check.Equals, true)
}
