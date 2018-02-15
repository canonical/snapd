// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/testutil"
)

type createUserSuite struct {
	testutil.BaseTest

	mockHome string
	restorer func()

	mockAddUser *testutil.MockCmd
	mockUserMod *testutil.MockCmd
}

var _ = check.Suite(&createUserSuite{})

func (s *createUserSuite) SetUpTest(c *check.C) {
	s.mockHome = c.MkDir()
	s.restorer = osutil.MockUserLookup(func(string) (*user.User, error) {
		current, err := user.Current()
		if err != nil {
			c.Fatalf("user.Current() failed with %s", err)
		}
		return &user.User{
			HomeDir: s.mockHome,
			Gid:     current.Gid,
			Uid:     current.Uid,
		}, nil
	})
	s.mockAddUser = testutil.MockCommand(c, "adduser", "")
	s.mockUserMod = testutil.MockCommand(c, "usermod", "")
}

func (s *createUserSuite) TearDownTest(c *check.C) {
	s.restorer()
	s.mockAddUser.Restore()
	s.mockUserMod.Restore()
}

func (s *createUserSuite) TestAddUserExtraUsersFalse(c *check.C) {
	err := osutil.AddUser("lakatos", &osutil.AddUserOptions{
		Gecos:      "my gecos",
		ExtraUsers: false,
	})
	c.Assert(err, check.IsNil)

	c.Check(s.mockAddUser.Calls(), check.DeepEquals, [][]string{
		{"adduser", "--force-badname", "--gecos", "my gecos", "--disabled-password", "lakatos"},
	})
}

func (s *createUserSuite) TestAddUserExtraUsersTrue(c *check.C) {
	err := osutil.AddUser("lakatos", &osutil.AddUserOptions{
		Gecos:      "my gecos",
		ExtraUsers: true,
	})
	c.Assert(err, check.IsNil)

	c.Check(s.mockAddUser.Calls(), check.DeepEquals, [][]string{
		{"adduser", "--force-badname", "--gecos", "my gecos", "--disabled-password", "--extrausers", "lakatos"},
	})
}

func (s *createUserSuite) TestAddSudoUser(c *check.C) {
	mockSudoers := c.MkDir()
	restorer := osutil.MockSudoersDotD(mockSudoers)
	defer restorer()

	err := osutil.AddUser("karl.sagan", &osutil.AddUserOptions{
		Gecos:      "my gecos",
		Sudoer:     true,
		ExtraUsers: true,
	})
	c.Assert(err, check.IsNil)

	c.Check(s.mockAddUser.Calls(), check.DeepEquals, [][]string{
		{"adduser", "--force-badname", "--gecos", "my gecos", "--disabled-password", "--extrausers", "karl.sagan"},
	})

	fs, _ := filepath.Glob(filepath.Join(mockSudoers, "*"))
	c.Assert(fs, check.HasLen, 1)
	c.Assert(filepath.Base(fs[0]), check.Equals, "create-user-karl%2Esagan")
	c.Check(fs[0], testutil.FileEquals, `
# Created by snap create-user

# User rules for karl.sagan
karl.sagan ALL=(ALL) NOPASSWD:ALL
`)
}

func (s *createUserSuite) TestAddUserSSHKeys(c *check.C) {
	err := osutil.AddUser("karl.sagan", &osutil.AddUserOptions{
		SSHKeys: []string{"ssh-key1", "ssh-key2"},
	})
	c.Assert(err, check.IsNil)
	c.Check(filepath.Join(s.mockHome, ".ssh", "authorized_keys"), testutil.FileEquals, "ssh-key1\nssh-key2")

}

func (s *createUserSuite) TestAddUserInvalidUsername(c *check.C) {
	err := osutil.AddUser("k!", nil)
	c.Assert(err, check.ErrorMatches, `cannot add user "k!": name contains invalid characters`)
}

func (s *createUserSuite) TestAddUserWithPassword(c *check.C) {
	mockSudoers := c.MkDir()
	restorer := osutil.MockSudoersDotD(mockSudoers)
	defer restorer()

	err := osutil.AddUser("karl.sagan", &osutil.AddUserOptions{
		Gecos:    "my gecos",
		Password: "$6$salt$hash",
	})
	c.Assert(err, check.IsNil)

	c.Check(s.mockAddUser.Calls(), check.DeepEquals, [][]string{
		{"adduser", "--force-badname", "--gecos", "my gecos", "--disabled-password", "karl.sagan"},
	})
	c.Check(s.mockUserMod.Calls(), check.DeepEquals, [][]string{
		{"usermod", "--password", "$6$salt$hash", "karl.sagan"},
	})

}

func (s *createUserSuite) TestRealUser(c *check.C) {
	oldUser := os.Getenv("SUDO_USER")
	defer func() { os.Setenv("SUDO_USER", oldUser) }()

	for _, t := range []struct {
		SudoUsername    string
		CurrentUsername string
		CurrentUid      int
	}{
		// simulate regular "root", no SUDO_USER set
		{"", os.Getenv("USER"), 0},
		// simulate a normal sudo invocation
		{"guy", "guy", 0},
		// simulate running "sudo -u some-user -i" as root
		// (LP: #1638656)
		{"root", os.Getenv("USER"), 1000},
	} {
		restore := osutil.MockUserCurrent(func() (*user.User, error) {
			return &user.User{
				Username: t.CurrentUsername,
				Uid:      strconv.Itoa(t.CurrentUid),
			}, nil
		})
		defer restore()

		os.Setenv("SUDO_USER", t.SudoUsername)
		cur, err := osutil.RealUser()
		c.Assert(err, check.IsNil)
		c.Check(cur.Username, check.Equals, t.CurrentUsername)
	}
}

func (s *createUserSuite) TestUidGid(c *check.C) {
	for k, t := range map[string]struct {
		User *user.User
		Uid  sys.UserID
		Gid  sys.GroupID
		Err  string
	}{
		"happy":   {&user.User{Uid: "10", Gid: "10"}, 10, 10, ""},
		"bad uid": {&user.User{Uid: "x", Gid: "10"}, sys.FlagID, sys.FlagID, "cannot parse user id x"},
		"bad gid": {&user.User{Uid: "10", Gid: "x"}, sys.FlagID, sys.FlagID, "cannot parse group id x"},
	} {
		uid, gid, err := osutil.UidGid(t.User)
		c.Check(uid, check.Equals, t.Uid, check.Commentf(k))
		c.Check(gid, check.Equals, t.Gid, check.Commentf(k))
		if t.Err == "" {
			c.Check(err, check.IsNil, check.Commentf(k))
		} else {
			c.Check(err, check.ErrorMatches, ".*"+t.Err+".*", check.Commentf(k))
		}
	}
}
