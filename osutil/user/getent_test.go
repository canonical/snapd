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

package user_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil/user"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type getentSuite struct {
	testutil.BaseTest

	getentDir  string
	mockGetent *testutil.MockCmd
}

var _ = Suite(&getentSuite{})

func (s *getentSuite) SetUpTest(c *C) {
	s.getentDir = c.MkDir()

	s.mockGetent = testutil.MockCommand(c, "getent", fmt.Sprintf(`
set -eu
base='%s'/"${1}${2:+/}${2-}"
cat "${base}"
if [ -f "${base}.exit" ]; then
  exit "$(cat "${base}.exit")"
fi
`, s.getentDir))
	s.AddCleanup(user.MockGetentSearchPath(s.mockGetent.BinDir() + ":" + user.DefaultGetentSearchPath))
	s.AddCleanup(s.mockGetent.Restore)
}

func (s *getentSuite) mockGetentOutput(c *C, value string, exit int, params ...string) {
	path := []string{s.getentDir}
	path = append(path, params...)
	resultPath := filepath.Join(path...)
	err := os.MkdirAll(filepath.Dir(resultPath), 0o755)
	c.Assert(err, IsNil)
	b := []byte(value)
	err = os.WriteFile(resultPath, b, 0o644)
	c.Assert(err, IsNil)
	if exit != 0 {
		exitBytes := []byte(strconv.Itoa(exit))
		err = os.WriteFile(resultPath+".exit", exitBytes, 0o644)
		c.Assert(err, IsNil)
	}
}

func (s *getentSuite) TestLookupGroupByName(c *C) {
	s.mockGetentOutput(c, `mygroup:x:60000:myuser,someuser
`, 0, "group", "mygroup")

	grp, err := user.LookupGroupFromGetent(user.GroupMatchGroupname("mygroup"))
	c.Assert(err, IsNil)
	c.Assert(grp, NotNil)
	c.Check(grp.Name, Equals, "mygroup")
	c.Check(grp.Gid, Equals, "60000")
}

func (s *getentSuite) TestLookupGroupByNameError(c *C) {
	_, err := user.LookupGroupFromGetent(user.GroupMatchGroupname("mygroup"))
	c.Assert(err, NotNil)
}

func (s *getentSuite) TestLookupGroupByNameDoesNotExist(c *C) {
	s.mockGetentOutput(c, ``, 2, "group", "mygroup")

	grp, err := user.LookupGroupFromGetent(user.GroupMatchGroupname("mygroup"))
	c.Assert(err, IsNil)
	c.Assert(grp, IsNil)
}

func (s *getentSuite) TestLookupGroupByNumericalName(c *C) {
	// This is probably not valid
	s.mockGetentOutput(c, `mygroup:x:60001:myuser,someuser
1mygroup:x:60000:myuser,someuser
`, 0, "group")

	grp, err := user.LookupGroupFromGetent(user.GroupMatchGroupname("1mygroup"))
	c.Assert(err, IsNil)
	c.Assert(grp, NotNil)
	c.Check(grp.Name, Equals, "1mygroup")
	c.Check(grp.Gid, Equals, "60000")
}

func (s *getentSuite) TestLookupUserByName(c *C) {
	s.mockGetentOutput(c, `johndoe:x:60000:60000:John Doe:/home/johndoe:/bin/bash
`, 0, "passwd", "johndoe")

	usr, err := user.LookupUserFromGetent(user.UserMatchUsername("johndoe"))
	c.Assert(err, IsNil)
	c.Assert(usr, NotNil)
	c.Check(usr.Username, Equals, "johndoe")
	c.Check(usr.Uid, Equals, "60000")
	c.Check(usr.Gid, Equals, "60000")
	c.Check(usr.Name, Equals, "John Doe")
	c.Check(usr.HomeDir, Equals, "/home/johndoe")
}

func (s *getentSuite) TestLookupUserByUid(c *C) {
	s.mockGetentOutput(c, `johndoe:x:60000:60000:John Doe:/home/johndoe:/bin/bash
`, 0, "passwd", "60000")

	usr, err := user.LookupUserFromGetent(user.UserMatchUid(60000))
	c.Assert(err, IsNil)
	c.Assert(usr, NotNil)
	c.Check(usr.Username, Equals, "johndoe")
	c.Check(usr.Uid, Equals, "60000")
	c.Check(usr.Gid, Equals, "60000")
	c.Check(usr.Name, Equals, "John Doe")
	c.Check(usr.HomeDir, Equals, "/home/johndoe")
}

func (s *getentSuite) TestLookupUserByNumericalName(c *C) {
	// This is probably not valid
	s.mockGetentOutput(c, `johndoe:x:60001:60001:John Doe2:/home/johndoe2:/bin/bash
1johndoe:x:60000:60000:John Doe:/home/johndoe:/bin/bash
`, 0, "passwd")

	usr, err := user.LookupUserFromGetent(user.UserMatchUsername("1johndoe"))
	c.Assert(err, IsNil)
	c.Assert(usr, NotNil)
	c.Check(usr.Username, Equals, "1johndoe")
	c.Check(usr.Uid, Equals, "60000")
	c.Check(usr.Gid, Equals, "60000")
	c.Check(usr.Name, Equals, "John Doe")
	c.Check(usr.HomeDir, Equals, "/home/johndoe")
}

func (s *getentSuite) TestLookupUserByNameMissing(c *C) {
	s.mockGetentOutput(c, ``, 2, "passwd", "johndoe")

	usr, err := user.LookupUserFromGetent(user.UserMatchUsername("johndoe"))
	c.Assert(err, IsNil)
	c.Assert(usr, IsNil)
}

func (s *getentSuite) TestLookupUserUidMissing(c *C) {
	s.mockGetentOutput(c, ``, 2, "passwd", "60000")

	usr, err := user.LookupUserFromGetent(user.UserMatchUid(60000))
	c.Assert(err, IsNil)
	c.Assert(usr, IsNil)
}

func (s *getentSuite) TestLookupGroupByNameMissing(c *C) {
	s.mockGetentOutput(c, ``, 2, "group", "mygroup")

	grp, err := user.LookupGroupFromGetent(user.GroupMatchGroupname("mygroup"))
	c.Assert(err, IsNil)
	c.Assert(grp, IsNil)
}

func (s *getentSuite) TestNoGetentBinary(c *C) {
	defer user.MockGetentSearchPath("/foo:/bar")()

	usr, err := user.LookupUserFromGetent(user.UserMatchUid(1000))
	c.Assert(err, ErrorMatches, "cannot locate getent executable")
	c.Assert(usr, IsNil)
}
