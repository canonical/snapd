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
	"io/ioutil"
	"os"
	"path/filepath"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/osutil/user"
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
	s.restorer = osutil.MockUserLookup(func(username string) (*user.User, error) {
		current, err := user.Current()
		if err != nil {
			c.Fatalf("user.Current() failed with %s", err)
		}
		return user.Fake(username, s.mockHome, current.UID(), current.GID()), nil
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
	bs, err := ioutil.ReadFile(fs[0])
	c.Assert(err, check.IsNil)
	c.Check(string(bs), check.Equals, `
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
	sshKeys, err := ioutil.ReadFile(filepath.Join(s.mockHome, ".ssh", "authorized_keys"))
	c.Assert(err, check.IsNil)
	c.Check(string(sshKeys), check.Equals, "ssh-key1\nssh-key2")

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
	if oldUser, ok := os.LookupEnv("SUDO_USER"); ok {
		defer func() { os.Setenv("SUDO_USER", oldUser) }()
	}

	curUser := os.Getenv("USER")
	if curUser == "" {
		c.Fatalf("$USER not set?!")
	}

	for _, t := range []struct {
		SudoUsername    string
		CurrentUsername string
		CurrentUid      sys.UserID
	}{
		// simulate regular "root", no SUDO_USER set
		{"", curUser, 0},
		// simulate a normal sudo invocation
		{"guy", "guy", 0},
		// simulate running "sudo -u some-user -i" as root
		// (LP: #1638656)
		{"root", curUser, 1000},
	} {
		restore := osutil.MockUserCurrent(func() (*user.User, error) {
			return user.Fake(t.CurrentUsername, "/home/"+t.CurrentUsername, t.CurrentUid, sys.GroupID(t.CurrentUid)), nil
		})
		defer restore()

		os.Setenv("SUDO_USER", t.SudoUsername)
		cur, err := osutil.RealUser()
		c.Assert(err, check.IsNil)
		c.Check(cur.Name(), check.Equals, t.CurrentUsername)
	}
}
